Rails.application.config.good_job.cron = {
  agent_orchestrator: {
    cron:        "* * * * *",
    class:       "AgentOrchestratorJob",
    description: "Wake up waiting live trading agents on alerts or heartbeat timer"
  }
}
