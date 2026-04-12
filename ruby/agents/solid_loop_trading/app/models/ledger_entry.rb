class LedgerEntry < ApplicationRecord
  belongs_to :trading_session
  belongs_to :trading_order, optional: true
end
