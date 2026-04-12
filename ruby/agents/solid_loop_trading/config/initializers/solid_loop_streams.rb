# config/initializers/solid_loop_streams.rb

Rails.application.config.to_prepare do
  ActiveSupport::Notifications.subscribe("solid_loop.message_created") do |name, start, finish, id, payload|
    message     = payload[:message]
    loop_record = message.loop

    if loop_record&.subject_type == "AgentRun"
      StreamBroadcastJob.perform_later("message_created",
        message_id:   message.id,
        execution_id: loop_record.subject_id
      )
    end
  end

  ActiveSupport::Notifications.subscribe("solid_loop.message_updated") do |name, start, finish, id, payload|
    message     = payload[:message]
    loop_record = message.loop

    if loop_record&.subject_type == "AgentRun"
      StreamBroadcastJob.perform_later("message_updated",
        message_id:   message.id,
        execution_id: loop_record.subject_id
      )
    end
  end

  ActiveSupport::Notifications.subscribe("solid_loop.loop_status_changed") do |name, start, finish, id, payload|
    loop_record = payload[:loop]

    if loop_record&.subject_type == "AgentRun"
      StreamBroadcastJob.perform_later("loop_status_changed",
        execution_id: loop_record.subject_id
      )

      Rails.logger.info "[App Subscriber] Status changed to #{loop_record.status} in SolidLoop #{loop_record.id} for Execution #{loop_record.subject_id}"

      agent_run = AgentRun.find_by(id: loop_record.subject_id)
      next unless agent_run

      case loop_record.status
      when "running"
        agent_run.orchestrator_running! unless agent_run.orchestrator_stopped?
      when "completed", "failed"
        Rails.logger.info "[App Subscriber] Loop finished for agent_run #{agent_run.id}"
        if agent_run.live?
          unless agent_run.orchestrator_stopped?
            agent_run.orchestrator_waiting!
            agent_run.update_column(:last_active_at, Time.current)
          end
        else
          agent_run.orchestrator_waiting! unless agent_run.orchestrator_stopped?
          agent_run.agent_batch&.check_and_start_next!
        end
      end
    end
  end
end
