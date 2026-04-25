import { type NodeProps } from '@xyflow/react'
import type { StageNodeState } from '../../lib/formatters'

export interface StageGraphLaneData extends Record<string, unknown> {
  label: string
  state: StageNodeState
}

export function StageGraphLane({ data }: NodeProps) {
  const lane_data = data as StageGraphLaneData
  return (
    <div className="stage-graph__lane" data-state={lane_data.state}>
      <span className="stage-graph__lane-label">{lane_data.label}</span>
    </div>
  )
}
