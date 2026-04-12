class AgentEventsController < ApplicationController
  def index
    @agent_events = SolidLoop::Event.order(id: :asc)

    if params[:loop_id]
      @agent_events = @agent_events.where(loop_id: params[:loop_id])
    end

    if params[:message_id]
      message = SolidLoop::Message.find(params[:message_id])
      tool_call_ids = []

      if message.role == "assistant"
        tool_call_ids = message.tool_calls.pluck(:id)
      elsif message.role == "tool" && message.tool_call_id.present?
        # Find the tool call record that this message is responding to
        tc = SolidLoop::ToolCall.joins(:message).find_by(
          solid_loop_messages: { loop_id: message.loop_id },
          tool_call_id: message.tool_call_id
        )
        tool_call_ids << tc.id if tc
      end

      @agent_events = @agent_events.where(
        "(eventable_type = 'SolidLoop::Message' AND eventable_id = :message_id) OR
         (eventable_type = 'SolidLoop::ToolCall' AND eventable_id IN (:tool_call_ids))",
        message_id: message.id,
        tool_call_ids: tool_call_ids.compact.uniq
      )

      # If it's an assistant message and we found no events, look for the completion that generated it
      if message.role == "assistant" && @agent_events.empty?
        prev_message = message.loop.messages
                              .where("created_at <= ?", message.created_at)
                              .where.not(id: message.id)
                              .order(created_at: :desc).first
        if prev_message
          @agent_events = SolidLoop::Event.where(eventable: prev_message, name: "llm_completion")
        end
      end
    end

    @agent_events = @agent_events.page(params[:page]).per(50) if @agent_events.respond_to?(:page)
  end

  def show
    @agent_event = SolidLoop::Event.find(params[:id])
  end
end
