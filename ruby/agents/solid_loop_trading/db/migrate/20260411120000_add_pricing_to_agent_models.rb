class AddPricingToAgentModels < ActiveRecord::Migration[8.1]
  def change
    add_column :agent_models, :price_per_input_token, :decimal, precision: 20, scale: 15, default: 0.0
    add_column :agent_models, :price_per_output_token, :decimal, precision: 20, scale: 15, default: 0.0
    add_column :agent_models, :price_per_cached_token, :decimal, precision: 20, scale: 15, default: 0.0
  end
end
