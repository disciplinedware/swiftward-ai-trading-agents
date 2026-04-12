class AddAgentModeToAgentBatches < ActiveRecord::Migration[8.0]
  def change
    add_column :agent_batches, :agent_mode, :string, default: "backtesting", null: false
  end
end
