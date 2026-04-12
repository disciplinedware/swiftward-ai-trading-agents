class AgentModelProvider < ApplicationRecord
  has_many :agent_models, dependent: :destroy

  validates :name, presence: true
  validates :base_url, presence: true
  validates :api_key, presence: true
end
