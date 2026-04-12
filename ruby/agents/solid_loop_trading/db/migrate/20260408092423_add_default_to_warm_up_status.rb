class AddDefaultToWarmUpStatus < ActiveRecord::Migration[8.1]
  def change
    AgentBatch.where(warm_up_status: nil).update_all(warm_up_status: 0)
    change_column_default :agent_batches, :warm_up_status, 0
  end
end
