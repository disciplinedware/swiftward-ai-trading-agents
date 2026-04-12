class MessagesController < ApplicationController
  before_action :set_agent_run
  before_action :set_message, only: [ :show, :regenerate, :fork, :edit, :update ]

  def show
    @message = @agent_run.loop.messages.includes(:events, :tool_calls).find(params[:id])

    # Events directly on the message (e.g., llm_completion, limit_reached)
    message_events = @message.events

    # Events on the tool calls of this message (e.g., tool_call, post requests)
    tool_call_ids = @message.tool_calls.pluck(:id)
    # If this is a tool message, include the events for its specific tool call
    tool_call_ids << @message.tool_call_id if @message.respond_to?(:tool_call_id) && @message.tool_call_id

    tool_events = SolidLoop::Event.where(eventable_type: "SolidLoop::ToolCall", eventable_id: tool_call_ids.compact.uniq)
    @all_events = (message_events + tool_events)

    # If it's an assistant message and we have no logs yet,
    # look for the completion event that generated it (attached to the preceding message)
    if @message.role == "assistant" && @all_events.empty?
      prev_message = @agent_run.loop.messages.where("created_at <= ?", @message.created_at)
                                        .where.not(id: @message.id)
                                        .order(created_at: :desc).first
      if prev_message
        @all_events += SolidLoop::Event.where(eventable: prev_message, name: "llm_completion")
      end
    end

    @all_events = @all_events.sort_by(&:created_at)
  end

  def edit
  end

  def update
    if @message.update(message_params)
      redirect_to @agent_run, notice: "Message updated successfully."
    else
      render :edit, status: :unprocessable_content
    end
  end

  def fork
    ActiveRecord::Base.transaction do
      # 1. Clone execution
      new_execution = @agent_run.dup
      new_execution.title = "Fork of #{@agent_run.title || 'Untitled'}"
      new_execution.save!

      new_loop = @agent_run.loop.dup
      new_loop.subject = new_execution
      new_loop.status = "paused"
      new_loop.state ||= {}
      new_loop.state["consecutive_mcp_failures"] = 0
      new_loop.save!

      # 2. Clone messages up to selected message
      messages_to_copy = @agent_run.loop.messages
                                            .where("id <= ?", @message.id)
                                            .order(id: :asc)

      # Keep track of old tool call IDs to map them in messages
      tool_call_mapping = {}

      messages_to_copy.each do |msg|
        new_msg = msg.dup
        new_msg.loop = new_loop
        new_msg.save!

        # 3. Clone tool calls for the message
        msg.tool_calls.each do |tc|
          new_tc = tc.dup
          new_tc.message = new_msg
          new_tc.save!
          tool_call_mapping[tc.id] = new_tc.id
        end

        # 4. If this is a tool message, link it to the newly cloned tool call
        if new_msg.role == "tool" && new_msg.respond_to?(:tool_call_id) && new_msg.tool_call_id.present?
          new_msg.update!(tool_call_id: tool_call_mapping[new_msg.tool_call_id])
        end
      end

      redirect_to new_execution, notice: "Execution forked successfully."
    end
  rescue => e
    redirect_to @agent_run, alert: "Failed to fork: #{e.message}"
  end

  def create
    @message = @agent_run.loop.messages.new(message_params)

    if @message.save
      # Resume the agent loop to process the new message
      @agent_run.agent&.resume!

      # Explicitly trigger the broadcast for manual messages to ensure immediate feedback
      ActiveSupport::Notifications.instrument("solid_loop.message_created", message: @message)

      respond_to do |format|
        format.turbo_stream
        format.html { redirect_to @agent_run }
      end
    else
      respond_to do |format|
        format.html { redirect_to @agent_run, alert: "Error creating message." }
      end
    end
  end

  def regenerate
    ActiveRecord::Base.transaction do
      # Delete this message and all messages after this one (though if it's last, it's just this one)
      @agent_run.loop.messages.where("id >= ?", @message.id).destroy_all
      @agent_run.loop.mcp_sessions.destroy_all # Clear state to be safe
      @agent_run.loop.update!(status: "paused")
    end

    # Re-trigger processing
    @agent_run.agent.resume!

    redirect_to @agent_run, notice: "Execution regenerated."
  end

  private

  def set_agent_run
    @agent_run = AgentRun.find(params[:agent_run_id])
  end

  def set_message
    @message = @agent_run.loop.messages.find(params[:id])
  end

  def message_params
    params.require(:message).permit(:role, :content)
  end
end
