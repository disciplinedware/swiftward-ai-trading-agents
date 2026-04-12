class CreateTradingScenarios < ActiveRecord::Migration[8.1]
  def change
    create_table :trading_scenarios do |t|
      t.string :name, null: false
      t.text :prompt, null: false
      t.text :user_prompt
      t.string :category
      t.string :filename
      t.string :trading_pair
      t.decimal :initial_balance
      t.datetime :start_at
      t.datetime :end_at
      t.integer :step_interval

      t.timestamps
    end
  end
end
