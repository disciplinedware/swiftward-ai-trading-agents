class CreateTradingSessions < ActiveRecord::Migration[8.1]
  def change
    create_table :trading_sessions do |t|
      t.string :uuid
      t.string :workdir_uuid
      t.jsonb :portfolio
      t.jsonb :open_orders
      t.integer :time_offset
      t.datetime :last_sync_at

      t.timestamps
    end

    add_index :trading_sessions, :uuid
  end
end
