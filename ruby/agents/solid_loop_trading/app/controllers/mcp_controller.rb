class McpController < ApplicationController
  skip_before_action :verify_authenticity_token
  
  TOOLS_MAPPING = {
    "set_time" => McpTools::SetTime,
    "get_candles" => McpTools::GetCandles,
    "add_order" => McpTools::AddOrder,
    "cancel_order" => McpTools::CancelOrder,
    "get_open_orders" => McpTools::GetOpenOrders,
    "get_portfolio" => McpTools::GetPortfolio,
    "get_news" => McpTools::GetNews,
    "add_asset" => McpTools::AddAsset,
    "finish_trade" => McpTools::FinishTrade,
    "get_order_history" => McpTools::GetOrderHistory
  }.freeze

  def call
    request_data = JSON.parse(request.body.read) rescue {}
    method = request_data["method"]
    params = request_data["params"] || {}
    
    case method
    when "initialize" then handle_initialize(params)
    when "tools/list" then handle_tools_list
    when "tools/call" then handle_tool_call(params, request_data["id"])
    else render_error(-32601, "Method not found")
    end
  rescue => e
    render_error(-32603, "Internal error: #{e.message}")
  end

  private

  def handle_initialize(params)
    # Check if we should reuse an existing session passed via headers
    existing_sid = request.headers["Mcp-Session-Id"]
    session = TradingSession.find_by(uuid: existing_sid) if existing_sid.present?
    
    if session
      Rails.logger.info "[McpController] Reusing existing session: #{session.uuid}"
    else
      # workdir_uuid is a shared ID for synchronizing file paths between MCPs
      # SolidLoop client sends it in 'initializationOptions'
      workdir_uuid = params.dig("initializationOptions", "workdir_uuid") || params.dig("options", "workdir_uuid")

      if workdir_uuid.present?
        session = TradingSession.find_or_create_by!(workdir_uuid: workdir_uuid) do |s|
          s.uuid = SecureRandom.uuid
        end
        Rails.logger.info "[McpController] #{session.previously_new_record? ? 'Created' : 'Found existing'} session: #{session.uuid} (workdir: #{workdir_uuid})"
      else
        session = TradingSession.create!(uuid: SecureRandom.uuid)
        Rails.logger.info "[McpController] Created new session: #{session.uuid} (no workdir)"
      end
    end
    
    response.set_header("Mcp-Session-Id", session.uuid)
    render json: { 
      jsonrpc: "2.0", 
      id: params["id"], 
      result: { 
        protocolVersion: "2024-11-05", 
        capabilities: { tools: {} }, 
        serverInfo: { name: "Trading-Demo-Server", version: "2.0.0" } 
      } 
    }
  end

  def handle_tools_list
    render json: {
      jsonrpc: "2.0",
      result: {
        tools: TOOLS_MAPPING.values.map { |tool_class| tool_class::SCHEMA }
      }
    }
  end

  def handle_tool_call(params, request_id)
    session = TradingSession.find_by(uuid: request.headers["Mcp-Session-Id"])
    return render_error(-32000, "Invalid session ID") unless session

    tool_name = params["name"]
    tool_class = TOOLS_MAPPING[tool_name]
    
    unless tool_class
      return render json: { jsonrpc: "2.0", id: request_id, result: { content: [{ type: "text", text: "Error: Tool not found" }] } }
    end

    result = tool_class.new(session, params["arguments"] || {}).call

    render json: {
      jsonrpc: "2.0",
      id: request_id,
      result: {
        content: [{ type: "text", text: result.is_a?(String) ? result : result.to_json }]
      }
    }
  end

  def render_error(code, message)
    render json: { jsonrpc: "2.0", error: { code: code, message: message } }, status: (code == -32000 ? 400 : 200)
  end
end
