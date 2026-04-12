class CreateAgentRuns < ActiveRecord::Migration[8.1]
  def change
    create_table :agent_runs do |t|
      t.references :agent_model, null: false, foreign_key: true
      t.references :agent_batch, foreign_key: true
      t.references :trading_scenario, foreign_key: true
      t.string :title
      t.string :status
      t.string :filename
      t.text :system_prompt
      t.text :user_prompt
      t.decimal :score

      t.timestamps
    end
  end
end
