class AgentModel < ApplicationRecord
  belongs_to :provider, class_name: "AgentModelProvider", foreign_key: "agent_model_provider_id"
  has_many :agent_runs, dependent: :destroy

  validates :llm_model_name, presence: true

  before_validation :set_defaults

  private

  def set_defaults
    self.tools ||= []
    if provider.present? && llm_model_name.present? && name.blank?
      self.name = "#{provider.name}: #{llm_model_name}"
    end
  end
end
