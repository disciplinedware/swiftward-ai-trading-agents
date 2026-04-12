class AddOrchestratorFieldsToAgentRuns < ActiveRecord::Migration[8.1]
  def change
    add_column :agent_runs, :orchestrator_status, :string, default: "initial", null: false
    add_column :agent_runs, :wakeup_interval_minutes, :integer, default: 15, null: false
    add_column :agent_runs, :last_active_at, :datetime
    add_column :agent_runs, :agent_id, :string
  end
end
