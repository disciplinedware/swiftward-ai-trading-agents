class CreateLedgerEntries < ActiveRecord::Migration[8.1]
  def change
    create_table :ledger_entries do |t|
      t.references :trading_session, null: false, foreign_key: true
      t.references :trading_order, foreign_key: true
      t.string :asset
      t.decimal :amount, precision: 24, scale: 8
      t.string :category

      t.timestamps
    end
  end
end
