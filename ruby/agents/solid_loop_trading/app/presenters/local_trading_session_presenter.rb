class LocalTradingSessionPresenter < TradingSessionPresenter
  def initialize(trading_session)
    @session = trading_session
  end

  def uuid = @session.uuid

  def initial_balance
    @initial_balance ||= begin
      deposit = all_ledger.select { |l| l.category == "deposit" && l.asset == "USDT" }.sum(&:amount).to_f
      deposit > 0 ? deposit : fallback_initial_balance
    end
  end

  def total_equity_usdt
    @total_equity_usdt ||= @session.total_equity_usdt
  end

  def total_equity_usdt_projected(offset_seconds:)
    @projected_equity ||= {}
    @projected_equity[offset_seconds] ||= @session.total_equity_usdt(at_time: virtual_now + offset_seconds)
  end

  def equity_snapshot
    @equity_snapshot ||= @session.equity_snapshot
  end

  def virtual_now
    @virtual_now ||= @session.virtual_now
  end

  def open_orders
    @open_orders ||= @session.open_orders || []
  end

  def chart_points
    @chart_points ||= begin
      points = [ { val: initial_balance, time: "Start" } ]
      orders_asc.each do |o|
        val = o.equity_at_execution.to_f
        val = initial_balance if val <= 0
        points << { val: val.round(2), time: order_time(o).strftime("%H:%M") }
      end
      points << { val: total_equity_usdt.round(2), time: "Now" }
      points
    end
  end

  def ledger_rows
    @ledger_rows ||= compute_ledger_rows
  end

  private

  def orders_asc
    @orders_asc ||= @session.trading_orders.order(id: :asc).to_a
  end

  def all_ledger
    @all_ledger ||= @session.ledger_entries.order(id: :asc).to_a
  end

  def order_time(order)
    order.executed_at || order.created_at
  end

  def price_at(symbol, time)
    symbol == "USDT" ? 1.0 : @session.market_price(symbol, at_time: time)
  end

  def cached_price_at(symbol, time, cache)
    key = [ symbol, time&.to_i ]
    cache.fetch(key) { cache[key] = price_at(symbol, time) }
  end

  def portfolio_equity(balances, at_time, price_cache)
    balances.sum { |s, a| a * cached_price_at(s, at_time, price_cache) }
  end

  def merge_balances(a, b)
    result = Hash.new(0.0).merge(a)
    b.each { |k, v| result[k] += v }
    result
  end

  def fallback_initial_balance
    mcp = SolidLoop::McpSession.find_by(session_id: @session.uuid)
    mcp&.loop&.subject&.trading_scenario&.initial_balance&.to_f || 100_000.0
  end

  # O(N) single pass: pre-groups ledger entries, then walks orders in order
  # accumulating balances incrementally instead of rescanning all_ledger per order.
  def compute_ledger_rows
    price_cache = {}

    linked_by_order = Hash.new { |h, k| h[k] = [] }
    unlinked = []
    all_ledger.each do |l|
      if l.trading_order_id.present?
        linked_by_order[l.trading_order_id.to_i] << l
      else
        unlinked << l
      end
    end

    # Reservations for pending orders lock USDT/assets but the funds are still yours
    # (the order hasn't executed). Add them back when computing equity.
    # We use DB status == "pending" rather than open_orders JSON, because orders can be
    # removed from the JSON without a release ledger entry (data inconsistency).
    open_order_db_ids = @session.trading_orders.where(status: "pending").pluck(:id).to_set

    # Two running unlinked accumulators:
    #   unlinked_strict  — entries with created_at < current order  (for balances_before)
    #   unlinked_loose   — entries with created_at <= current order (for balances_after)
    # We advance both pointers as we iterate orders chronologically.
    linked_running        = Hash.new(0.0)
    unlinked_strict       = Hash.new(0.0)
    unlinked_loose        = Hash.new(0.0)
    strict_ptr            = 0
    loose_ptr             = 0
    prev_equity_after     = nil
    # Addback for open-order reservations applied so far (mirrors total_equity_usdt logic)
    equity_addback        = Hash.new(0.0)

    orders_asc.each_with_index.map do |order, idx|
      time = order_time(order)

      # before: only linked entries from *previous* orders, and unlinked entries strictly before
      while strict_ptr < unlinked.size && unlinked[strict_ptr].created_at < order.created_at
        unlinked_strict[unlinked[strict_ptr].asset] += unlinked[strict_ptr].amount.to_f
        strict_ptr += 1
      end

      balances_before = merge_balances(linked_running, unlinked_strict)
      equity_before   = portfolio_equity(merge_balances(balances_before, equity_addback), time, price_cache)

      # Add this order's linked entries after computing before-balances
      linked_by_order[order.id].each { |l| linked_running[l.asset] += l.amount.to_f }

      # If this is an open order, its reservation is in the ledger but the funds are still ours —
      # add back the locked amount so equity stays consistent with total_equity_usdt
      if open_order_db_ids.include?(order.id)
        linked_by_order[order.id].select { |l| l.category == "reservation" }.each do |l|
          equity_addback[l.asset] -= l.amount.to_f  # reservation is negative → addback is positive
        end
      end

      # after: linked entries up to and including this order, and unlinked entries up to <= order time
      while loose_ptr < unlinked.size && unlinked[loose_ptr].created_at <= order.created_at
        unlinked_loose[unlinked[loose_ptr].asset] += unlinked[loose_ptr].amount.to_f
        loose_ptr += 1
      end

      balances_after = merge_balances(linked_running, unlinked_loose)
      equity_after   = portfolio_equity(merge_balances(balances_after, equity_addback), time, price_cache)

      wait_pnl, wait_duration = if idx > 0
        prev = orders_asc[idx - 1]
        [ equity_before - prev_equity_after, time - order_time(prev) ]
      else
        [ equity_before - initial_balance, nil ]
      end

      prev_equity_after = equity_after

      LedgerRow.new(
        executed_at:     time,
        side:            order.side,
        amount:          order.amount,
        symbol:          order.symbol,
        price:           order.price,
        status:          order.status,
        reasoning:       order.reasoning,
        balances_before: balances_before,
        equity_before:   equity_before,
        balances_after:  balances_after,
        equity_after:    equity_after,
        impact:          equity_after - equity_before,
        total_pnl:       equity_after - initial_balance,
        wait_pnl:        wait_pnl,
        wait_duration:   wait_duration
      )
    end
  end
end
