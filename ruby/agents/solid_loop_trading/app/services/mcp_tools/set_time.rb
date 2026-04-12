module McpTools
  class SetTime < BaseTool
    SCHEMA = {
      name: "set_time",
      description: "Set the exchange clock to the specified timestamp.",
      inputSchema: {
        type: "object",
        properties: {
          timestamp: { type: ["string", "integer"], description: "Target time. Prefer ISO 8601 string (e.g. '2025-02-15T14:00:00Z'). Unix timestamp (integer) also accepted." }
        },
        required: ["timestamp"]
      }
    }.freeze

    def call
      raw = arguments["timestamp"]
      ts = (raw.is_a?(Integer) || raw.to_s.match?(/\A\d+\z/)) ? raw.to_i : Time.parse(raw.to_s).utc.to_i
      target_time = Time.at(ts)
      
      # Strict check: cannot move time backwards
      if session.time_offset.present? && target_time < session.virtual_now
        return "Error: Cannot move time backwards. Current time: #{session.virtual_now.utc}"
      end

      session.set_virtual_time(target_time)
      "Success: Clock set to #{session.virtual_now.utc}"
    end
  end
end
