import { useEffect, useMemo, useState } from 'react'
import {
  Background,
  ReactFlow,
  type Edge,
  type Node,
  type NodeTypes,
} from '@xyflow/react'
import {
  buildStageDagTopology,
  type DecisionsSummary,
  type RunStage,
  type RunStatus,
  type StageDagEdgeState,
} from '../../lib/formatters'
import {
  StageGraphNode,
  type StageGraphNodeData,
} from './StageGraphNode'
import {
  StageGraphLane,
  type StageGraphLaneData,
} from './StageGraphLane'

interface StageGraphViewProps {
  stage: RunStage
  status: RunStatus
  decisions_summary?: DecisionsSummary | null
}

const NODE_TYPES: NodeTypes = {
  stage: StageGraphNode,
  lane: StageGraphLane,
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

const VERTICAL_EDGES: ReadonlySet<string> = new Set([
  'research__structure',
  'structure__write',
  'write__visual_break',
  'visual_break__review',
  'review__critic',
  'critic__scenario_review',
  'assemble__metadata_ack',
])

/* Lifecycle lanes (queue/terminal) are filtered out of the DAG render —
   they're not work phases. Same rationale as the stepper trim. */
const HIDDEN_LANES: ReadonlySet<string> = new Set(['pending', 'complete'])
const HIDDEN_NODES: ReadonlySet<string> = new Set(['pending', 'complete'])

export function StageGraphView({
  stage,
  status,
  decisions_summary,
}: StageGraphViewProps) {
  const reduced_motion = usePrefersReducedMotion()

  const { rf_nodes, rf_edges } = useMemo(() => {
    const dag = buildStageDagTopology(stage, status, decisions_summary)

    const visible_lanes = dag.lanes.filter((lane) => !HIDDEN_LANES.has(lane.id))
    const visible_stage_nodes = dag.nodes.filter(
      (node) => !HIDDEN_NODES.has(node.id),
    )
    const visible_edges = dag.edges.filter(
      (edge) =>
        !HIDDEN_NODES.has(edge.source) && !HIDDEN_NODES.has(edge.target),
    )

    /* Lanes carry absolute x positions from buildStageDagTopology. After
       dropping the leftmost (pending) lane, shift remaining lanes left so
       the canvas starts at x=0 instead of leaving a gap. */
    const x_offset =
      visible_lanes.length > 0
        ? Math.min(...visible_lanes.map((l) => l.x))
        : 0

    const lane_nodes: Node<StageGraphLaneData>[] = visible_lanes.map((lane) => ({
      id: `lane-${lane.id}`,
      type: 'lane',
      position: { x: lane.x - x_offset, y: 0 },
      data: { label: lane.label, state: lane.state },
      style: { width: lane.width, height: lane.height },
      draggable: false,
      selectable: false,
      connectable: false,
      zIndex: 0,
    }))

    const stage_nodes: Node<StageGraphNodeData>[] = visible_stage_nodes.map((node) => ({
      id: node.id,
      type: 'stage',
      parentId: `lane-${node.parent}`,
      extent: 'parent',
      position: { x: node.rel_x, y: node.rel_y },
      data: {
        label: node.label,
        state: node.state,
        counter: node.counter,
      },
      draggable: false,
      selectable: false,
      connectable: false,
      zIndex: 1,
    }))

    const edges: Edge[] = visible_edges.map((edge) => {
      const is_vertical = VERTICAL_EDGES.has(edge.id)
      return {
        id: edge.id,
        source: edge.source,
        target: edge.target,
        sourceHandle: is_vertical ? 'bottom' : 'right',
        targetHandle: is_vertical ? 'top' : 'left',
        animated:
          !reduced_motion &&
          (edge.state as StageDagEdgeState) === 'active',
        className: `stage-graph__edge stage-graph__edge--${edge.state}`,
        selectable: false,
      }
    })

    return {
      rf_nodes: [...lane_nodes, ...stage_nodes],
      rf_edges: edges,
    }
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
        fitViewOptions={{ padding: 0.1 }}
        nodesDraggable={false}
        nodesConnectable={false}
        elementsSelectable={false}
        zoomOnScroll={false}
        zoomOnPinch={false}
        zoomOnDoubleClick={false}
        panOnScroll={false}
        panOnDrag={false}
        minZoom={0.1}
        maxZoom={2}
        proOptions={{ hideAttribution: true }}
      >
        <Background gap={20} size={1} />
      </ReactFlow>
    </div>
  )
}
