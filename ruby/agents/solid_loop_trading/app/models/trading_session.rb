class TradingSession < ApplicationRecord
  has_many :trading_orders, dependent: :destroy
  has_many :ledger_entries, dependent: :destroy
  validates :uuid, presence: true, uniqueness: true

  DEFAULT_SPREAD = 0.001 # 0.1% for Market Orders

  def virtual_now
    return Time.current unless time_offset
    Time.at(Time.current.to_i + time_offset)
  end

  def sync_state!
    transaction do
      lock!
      if last_sync_at.nil?
        update!(last_sync_at: virtual_now)
        return
      end

      current_v_now = virtual_now
      if current_v_now > last_sync_at
        process_filled_orders(last_sync_at, current_v_now)
        update!(last_sync_at: current_v_now)
      end
    end
  end

  def set_virtual_time(target_time)
    transaction do
      lock!
      sync_state!
      new_offset = target_time.to_i - Time.current.to_i
      update!(time_offset: new_offset, last_sync_at: target_time)
    end
  end

  def get_balance(symbol)
    LedgerEntry.where(trading_session_id: id, asset: symbol.to_s).sum(:amount).to_f.round(8)
  end

  def portfolio
    LedgerEntry.where(trading_session_id: id).group(:asset).sum(:amount).transform_values { |v| v.to_f.round(8) }.reject { |_, v| v == 0 }
  end

  def get_reserved_balance(symbol)
    orders = open_orders || []
    if symbol.to_s == "USDT"
      orders.select { |o| o["side"] == "buy" }.sum { |o| o["amount"].to_f * o["price"].to_f }
    else
      orders.select { |o| o["side"] == "sell" && o["symbol"] == symbol.to_s }.sum { |o| o["amount"].to_f }
    end
  end

  def add_asset(symbol, amount, category: "adjustment", order: nil)
    ledger_entries.create!(
      asset: symbol.to_s,
      amount: amount,
      category: category,
      trading_order: order
    )
  end

  def market_price(symbol, at_time: nil)
    at_time ||= virtual_now
    candle = OhlcCandle.for_symbol(symbol).where("timestamp <= ?", at_time).order(timestamp: :desc).first
    candle&.close&.to_f
  end

  def execute_market_trade(symbol:, side:, amount:, reasoning: nil)
    transaction do
      lock!
      sync_state!
      price = market_price(symbol)
      return { error: "Price not found" } unless price

      exec_price = (side.to_s == "buy") ? price * (1 + DEFAULT_SPREAD) : price * (1 - DEFAULT_SPREAD)
      res = process_transaction(symbol, side, amount, exec_price, reasoning: reasoning)
      res.is_a?(Hash) ? res.merge(price: exec_price) : res
    end
  end

  def place_limit_order(symbol:, side:, amount:, price:, post_only: false, reasoning: nil)
    transaction do
      lock!
      sync_state!
      v_now = virtual_now

      if side.to_s == "buy"
        cost = (amount * price).round(8)
        available = get_balance("USDT")
        return { error: "Insufficient USDT (Available: #{available}, Required: #{cost})" } if available < cost
      else
        available = get_balance(symbol)
        return { error: "Insufficient #{symbol} (Available: #{available}, Required: #{amount})" } if available < amount
      end

      order_id = SecureRandom.hex(4)

      order_record = trading_orders.create!(
        order_id: order_id,
        symbol: symbol,
        side: side,
        amount: amount,
        price: price,
        status: "pending",
        executed_at: v_now, # Time of placement for pending orders
        reasoning: reasoning
      )

      if side.to_s == "buy"
        add_asset("USDT", -cost, category: "reservation", order: order_record)
      else
        add_asset(symbol, -amount, category: "reservation", order: order_record)
      end
      
      order_record.update!(equity_at_execution: total_equity_usdt)

      order_data = {
        id: order_id,
        symbol: symbol,
        side: side,
        amount: amount,
        price: price,
        post_only: post_only,
        status: "pending",
        created_at: v_now.to_i
      }

      orders = (open_orders || []) + [order_data]
      update!(open_orders: orders)
      { success: true, order_id: order_id, order: order_data }
    end
  end

  def cancel_order(order_id, reasoning: nil)
    transaction do
      lock!
      sync_state!
      orders = open_orders || []
      order = orders.find { |o| o["id"] == order_id }
      return { error: "Order not found" } unless order

      order_record = trading_orders.find_by(order_id: order_id)

      if order["side"] == "buy"
        add_asset("USDT", (order["amount"].to_f * order["price"].to_f).round(8), category: "release", order: order_record)
      else
        add_asset(order["symbol"], order["amount"].to_f, category: "release", order: order_record)
      end

      order_record&.update!(status: "cancelled", executed_at: virtual_now, reasoning: reasoning)

      update!(open_orders: orders.reject { |o| o["id"] == order_id })
      { success: true }
    end
  end

  def total_equity_usdt(at_time: nil)
    v_now = at_time || virtual_now
    
    # 1. Get free balances from Ledger
    # We sum all entries up to the current real time to be safe
    balances = LedgerEntry.where(trading_session_id: id).group(:asset).sum(:amount)
    
    # 2. Get reserved balances from pending orders (DB status is source of truth —
    # open_orders JSON can get out of sync when orders are dropped without a release entry).
    pending_orders = trading_orders.where(status: "pending")

    total = 0.0

    # 3. Calculate value of ALL assets (Free + Reserved)
    all_assets = (balances.keys + pending_orders.pluck(:symbol) + ["USDT"]).uniq

    all_assets.each do |symbol|
      free_amount = balances[symbol].to_f

      # Calculate reserved for this symbol from pending orders
      reserved_amount = 0.0
      if symbol == "USDT"
        reserved_amount = pending_orders.select { |o| o.side == "buy" }.sum { |o| o.amount.to_f * o.price.to_f }
      else
        reserved_amount = pending_orders.select { |o| o.side == "sell" && o.symbol == symbol }.sum { |o| o.amount.to_f }
      end
      
      total_amount = free_amount + reserved_amount
      next if total_amount == 0
      
      price = (symbol == "USDT") ? 1.0 : market_price(symbol, at_time: v_now)
      total += total_amount * (price || 0.0)
    end
    
    total.round(2)
  end

  def equity_snapshot
    balances = portfolio
    orders = open_orders || []
    v_now = virtual_now
    
    snapshot = {}
    # USDT is always 1:1
    usdt_bal = get_balance("USDT")
    usdt_res = get_reserved_balance("USDT")
    snapshot["USDT"] = { amount: (usdt_bal + usdt_res).round(2), value_usdt: (usdt_bal + usdt_res).round(2) }

    balances.each do |symbol, amount|
      next if symbol == "USDT"
      reserved = get_reserved_balance(symbol)
      total_amount = (amount.to_f + reserved).round(8)
      price = market_price(symbol, at_time: v_now) || 0.0
      snapshot[symbol] = { 
        amount: total_amount, 
        value_usdt: (total_amount * price).round(2) 
      }
    end
    snapshot
  end

  private

  def process_filled_orders(start_time, end_time)
    orders = open_orders || []
    return if orders.empty?

    filled_order_ids = []
    remaining_orders = []
    v_now = virtual_now

    orders.each do |order|
      candles = OhlcCandle.for_symbol(order["symbol"]).where(timestamp: start_time..end_time)
      next if candles.empty?

      is_filled = (order["side"] == "buy") ? 
                  candles.any? { |c| c.low.to_f <= order["price"].to_f } : 
                  candles.any? { |c| c.high.to_f >= order["price"].to_f }

      if is_filled
        order_record = trading_orders.find_by(order_id: order["id"])
        
        if order["side"] == "buy"
          add_asset(order["symbol"], order["amount"], category: "fill", order: order_record)
        else
          add_asset("USDT", (order["amount"].to_f * order["price"].to_f).round(8), category: "fill", order: order_record)
        end
        
        order_record&.update!(status: "filled", executed_at: v_now)
        filled_order_ids << order["id"]
      else
        remaining_orders << order
      end
    end

    if filled_order_ids.any?
      update!(open_orders: remaining_orders)
      # Snapshots are still useful as a fallback, but UI will prefer Live Calculation
      new_equity = total_equity_usdt
      trading_orders.where(order_id: filled_order_ids).update_all(equity_at_execution: new_equity)
    end
  end

  def process_transaction(symbol, side, amount, price, reasoning: nil)
    v_now = virtual_now
    order_record = trading_orders.create!(
      order_id: SecureRandom.hex(4),
      symbol: symbol,
      side: side,
      amount: amount,
      price: price,
      status: "filled",
      executed_at: v_now,
      reasoning: reasoning
    )

    if side.to_s == "buy"
      cost = (amount * price).round(8)
      available = get_balance("USDT")
      raise "Insufficient USDT" if available < cost
      
      add_asset("USDT", -cost, category: "trade", order: order_record)
      add_asset(symbol, amount, category: "trade", order: order_record)
      change_usdt = -cost
      change_asset = amount
    else
      available = get_balance(symbol)
      raise "Insufficient #{symbol}" if available < amount
      
      revenue = (amount * price).round(8)
      add_asset(symbol, -amount, category: "trade", order: order_record)
      add_asset("USDT", revenue, category: "trade", order: order_record)
      change_usdt = revenue
      change_asset = -amount
    end

    order_record.update!(equity_at_execution: total_equity_usdt)

    { success: true, price: price, change_usdt: change_usdt, change_asset: change_asset, symbol: symbol }
  end
end
