class CreateAgentBatches < ActiveRecord::Migration[8.1]
  def change
    create_table :agent_batches do |t|
      t.references :agent_model, null: false, foreign_key: true
      t.string :prefix
      t.string :status
      t.string :hardware
      t.integer :attempts
      t.integer :concurrency
      t.text :conclusion
      t.datetime :start_at
      t.datetime :end_at
      t.integer :step_interval
      t.integer :warm_up_status
      t.datetime :warm_up_at
      t.text :warm_up_response

      t.timestamps
    end
  end
end
