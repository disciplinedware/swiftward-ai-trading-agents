require 'rails_helper'

RSpec.describe LedgerEntry, type: :model do
  let(:session) { TradingSession.create!(uuid: SecureRandom.uuid) }
  let(:order) { session.trading_orders.create!(order_id: 'test', symbol: 'BTCUSD', side: 'buy', amount: 1, price: 50000, status: 'filled') }

  describe "validations" do
    it "is valid with a session, asset, amount and category" do
      entry = LedgerEntry.new(
        trading_session: session,
        asset: "USDT",
        amount: 1000.0,
        category: "deposit"
      )
      expect(entry).to be_valid
    end

    it "can optionally belong to a trading order" do
      entry = LedgerEntry.new(
        trading_session: session,
        trading_order: order,
        asset: "BTCUSD",
        amount: 1.0,
        category: "trade"
      )
      expect(entry).to be_valid
    end
  end

  describe "scopes and associations" do
    it "belongs to a trading session" do
      entry = session.ledger_entries.create!(asset: "USDT", amount: 100, category: "deposit")
      expect(entry.trading_session).to eq(session)
    end

    it "belongs to an optional trading order" do
      entry = session.ledger_entries.create!(asset: "BTCUSD", amount: 1, category: "trade", trading_order: order)
      expect(entry.trading_order).to eq(order)
    end
  end
end
