class CreateTradingOrders < ActiveRecord::Migration[8.1]
  def change
    create_table :trading_orders do |t|
      t.references :trading_session, null: false, foreign_key: true
      t.string :order_id
      t.string :symbol
      t.string :side
      t.decimal :amount
      t.decimal :price
      t.string :status
      t.decimal :equity_at_execution
      t.datetime :executed_at
      t.text :reasoning

      t.timestamps
    end
  end
end
