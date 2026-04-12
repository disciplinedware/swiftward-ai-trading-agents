class StreamBroadcastJob < ApplicationJob
  include GoodJob::ActiveJobExtensions::Concurrency

  queue_as :broadcasts

  good_job_control_concurrency_with(
    key: -> {
      event = arguments[0]
      ids   = arguments[1]
      case event
      when "message_updated"     then "stream_broadcast-message_updated-#{ids[:message_id]}"
      when "loop_status_changed" then "stream_broadcast-loop_status_changed-#{ids[:execution_id]}"
      else "stream_broadcast-#{event}-#{ids.values.join('-')}"
      end
    },
    enqueue_limit: 1
  )

  def perform(event, **ids)
    case event
    when "message_created"
      message   = SolidLoop::Message.find_by(id: ids[:message_id])
      execution = AgentRun.find_by(id: ids[:execution_id])
      return unless message && execution

      Turbo::StreamsChannel.broadcast_append_to(
        execution,
        target: "messages",
        partial: "solid_loop/messages/message",
        locals: { message: message, agent_run: execution }
      )

    when "message_updated"
      message   = SolidLoop::Message.find_by(id: ids[:message_id])
      execution = AgentRun.find_by(id: ids[:execution_id])
      return unless message && execution

      Turbo::StreamsChannel.broadcast_replace_to(
        execution,
        target: ActionView::RecordIdentifier.dom_id(message),
        partial: "solid_loop/messages/message",
        locals: { message: message, agent_run: execution }
      )

    when "loop_status_changed"
      execution = AgentRun.find_by(id: ids[:execution_id])
      return unless execution

      Turbo::StreamsChannel.broadcast_update_to(
        execution,
        target: "agent_run_status",
        partial: "agent_runs/status",
        locals: { conversation: execution }
      )
    end
  end
end
