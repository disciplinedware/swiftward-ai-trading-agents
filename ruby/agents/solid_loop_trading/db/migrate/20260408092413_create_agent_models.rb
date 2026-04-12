class CreateAgentModels < ActiveRecord::Migration[8.1]
  def change
    create_table :agent_models do |t|
      t.references :agent_model_provider, foreign_key: true
      t.string :name
      t.string :llm_model_name
      t.text :description
      t.jsonb :tools
      t.jsonb :data

      t.timestamps
    end
  end
end
