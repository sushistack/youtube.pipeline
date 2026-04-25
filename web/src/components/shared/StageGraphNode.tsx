import { Handle, Position, type NodeProps } from '@xyflow/react'
import type { StageNodeCounter, StageNodeState } from '../../lib/formatters'

export interface StageGraphNodeData extends Record<string, unknown> {
  label: string
  state: StageNodeState
  counter?: StageNodeCounter
}

export function StageGraphNode({ data }: NodeProps) {
  const node_data = data as StageGraphNodeData
  return (
    <div
      className="stage-graph__node"
      data-state={node_data.state}
      aria-label={`${node_data.label}: ${node_data.state}`}
    >
      <Handle
        type="target"
        id="left"
        position={Position.Left}
        className="stage-graph__handle"
        isConnectable={false}
      />
      <Handle
        type="target"
        id="top"
        position={Position.Top}
        className="stage-graph__handle"
        isConnectable={false}
      />
      <span className="stage-graph__node-dot" aria-hidden="true" />
      <div className="stage-graph__node-body">
        <span className="stage-graph__node-label">{node_data.label}</span>
        {node_data.counter ? (
          <span className="stage-graph__node-counter">
            {node_data.counter.done}/{node_data.counter.total}{' '}
            {node_data.counter.suffix}
          </span>
        ) : null}
      </div>
      <Handle
        type="source"
        id="right"
        position={Position.Right}
        className="stage-graph__handle"
        isConnectable={false}
      />
      <Handle
        type="source"
        id="bottom"
        position={Position.Bottom}
        className="stage-graph__handle"
        isConnectable={false}
      />
    </div>
  )
}
