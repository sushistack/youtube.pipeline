import { useEffect, useMemo, useState } from 'react'
import {
  Background,
  ReactFlow,
  type Edge,
  type Node,
  type NodeTypes,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import {
  buildStageDagTopology,
  type DecisionsSummary,
  type RunStage,
  type RunStatus,
} from '../../lib/formatters'
import { layoutStageDag } from '../../lib/dagLayout'
import {
  StageGraphNode,
  type StageGraphNodeData,
} from './StageGraphNode'

interface StageGraphViewProps {
  stage: RunStage
  status: RunStatus
  decisions_summary?: DecisionsSummary | null
}

const NODE_TYPES: NodeTypes = {
  stage: StageGraphNode,
}

function usePrefersReducedMotion() {
  const [reduced, set_reduced] = useState(() => {
    if (typeof window === 'undefined' || !window.matchMedia) {
      return false
    }
    return window.matchMedia('(prefers-reduced-motion: reduce)').matches
  })

  useEffect(() => {
    if (typeof window === 'undefined' || !window.matchMedia) {
      return
    }
    const query = window.matchMedia('(prefers-reduced-motion: reduce)')
    const handler = (event: MediaQueryListEvent) => {
      set_reduced(event.matches)
    }
    query.addEventListener('change', handler)
    return () => {
      query.removeEventListener('change', handler)
    }
  }, [])

  return reduced
}

export function StageGraphView({
  stage,
  status,
  decisions_summary,
}: StageGraphViewProps) {
  const reduced_motion = usePrefersReducedMotion()

  const { rf_nodes, rf_edges } = useMemo(() => {
    const dag = buildStageDagTopology(stage, status, decisions_summary)
    const positioned = layoutStageDag(dag.nodes, dag.edges)

    const nodes: Node<StageGraphNodeData>[] = positioned.map((node) => ({
      id: node.id,
      type: 'stage',
      position: { x: node.x, y: node.y },
      data: {
        label: node.label,
        state: node.state,
        counter: node.counter,
      },
      draggable: false,
      selectable: false,
      connectable: false,
    }))

    const edges: Edge[] = dag.edges.map((edge) => ({
      id: edge.id,
      source: edge.source,
      target: edge.target,
      animated: !reduced_motion && edge.state === 'active',
      data: { state: edge.state },
      className: `stage-graph__edge stage-graph__edge--${edge.state}`,
      selectable: false,
    }))

    return { rf_nodes: nodes, rf_edges: edges }
  }, [stage, status, decisions_summary, reduced_motion])

  return (
    <div
      className="stage-graph"
      data-reduced-motion={String(reduced_motion)}
      aria-label="Pipeline DAG"
      role="img"
    >
      <ReactFlow
        nodes={rf_nodes}
        edges={rf_edges}
        nodeTypes={NODE_TYPES}
        fitView
        fitViewOptions={{ padding: 0.15 }}
        nodesDraggable={false}
        nodesConnectable={false}
        elementsSelectable={false}
        zoomOnScroll={false}
        zoomOnPinch={false}
        zoomOnDoubleClick={false}
        panOnScroll={false}
        panOnDrag={false}
        proOptions={{ hideAttribution: true }}
      >
        <Background gap={24} size={1} />
      </ReactFlow>
    </div>
  )
}
