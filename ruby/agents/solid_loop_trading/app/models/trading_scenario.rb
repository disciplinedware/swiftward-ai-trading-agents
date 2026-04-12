class TradingScenario < ApplicationRecord

  has_many :agent_runs, dependent: :nullify

  validates :name, presence: true
  validates :prompt, presence: true
end
