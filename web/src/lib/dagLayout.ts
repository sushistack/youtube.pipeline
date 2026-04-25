import dagre from 'dagre'
import type { StageDagEdgeModel, StageDagNodeModel } from './formatters'

export interface PositionedDagNode extends StageDagNodeModel {
  x: number
  y: number
}

export interface DagLayoutOptions {
  direction?: 'LR' | 'TB'
  node_width?: number
  node_height?: number
  rank_sep?: number
  node_sep?: number
}

export function layoutStageDag(
  nodes: StageDagNodeModel[],
  edges: StageDagEdgeModel[],
  options: DagLayoutOptions = {},
): PositionedDagNode[] {
  const {
    direction = 'LR',
    node_width = 168,
    node_height = 56,
    rank_sep = 56,
    node_sep = 28,
  } = options

  const graph = new dagre.graphlib.Graph()
  graph.setGraph({ rankdir: direction, ranksep: rank_sep, nodesep: node_sep })
  graph.setDefaultEdgeLabel(() => ({}))

  for (const node of nodes) {
    graph.setNode(node.id, { width: node_width, height: node_height })
  }
  for (const edge of edges) {
    graph.setEdge(edge.source, edge.target)
  }

  dagre.layout(graph)

  return nodes.map((node) => {
    const { x, y } = graph.node(node.id)
    return {
      ...node,
      x: x - node_width / 2,
      y: y - node_height / 2,
    }
  })
}
