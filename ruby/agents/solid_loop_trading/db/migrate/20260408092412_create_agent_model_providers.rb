class CreateAgentModelProviders < ActiveRecord::Migration[8.1]
  def change
    create_table :agent_model_providers do |t|
      t.string :name, null: false
      t.string :base_url, null: false
      t.string :api_key, null: false
      t.text :description

      t.timestamps
    end
  end
end
