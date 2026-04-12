class AddWakeupIntervalToAgentBatches < ActiveRecord::Migration[8.0]
  def change
    add_column :agent_batches, :wakeup_interval_minutes, :integer, default: 15, null: false
  end
end
