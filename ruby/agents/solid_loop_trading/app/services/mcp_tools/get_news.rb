module McpTools
  class GetNews < BaseTool
    SCHEMA = {
      name: "get_news",
      description: "Search recent market news and announcements. Only news published up to the current time is returned.",
      inputSchema: {
        type: "object",
        properties: {
          keywords: { type: "string", description: "Search query or keywords (e.g. 'bitcoin ETF', 'fed interest rates')" },
          limit: { type: "integer", default: 10, description: "Number of news items to return." },
          reasoning: { type: "string", description: "What you are looking for and why (e.g. 'checking for macro events before taking a position', 'looking for news that explains the recent price spike')" }
        }
      }
    }.freeze

    def call
      session.sync_state!
      
      keywords = arguments["keywords"]
      limit = (arguments["limit"] || 10).to_i
      v_now = session.virtual_now

      query = NewsItem.where("published_at <= ?", v_now)
                     .where("edited_at IS NULL OR edited_at <= ?", v_now)
                     .order(published_at: :desc)

      if keywords.present?
        # Split keywords into words and search for any of them (OR)
        words = keywords.split(/\s+/).reject(&:blank?)
        if words.any?
          conditions = []
          values = []
          
          words.each do |word|
            pattern = "%#{word}%"
            conditions << "(title ILIKE ? OR content ILIKE ?)"
            values << pattern
            values << pattern
          end
          
          query = query.where(conditions.join(" OR "), *values)
        end
      end

      news = query.limit(limit)

      if news.empty?
        return "No news found#{" for '#{keywords}'" if keywords.present?} before #{v_now.utc}"
      end

      result = ["Recent News (#{v_now.utc}):"]
      news.each do |item|
        edited_note = item.edited_at.present? ? " [EDITED #{item.edited_at.utc.strftime('%Y-%m-%d %H:%M')}]" : ""
        result << "[#{item.published_at.utc}]#{edited_note} #{item.title}"
        result << "#{item.content}\n" if item.content.present?
      end

      result.join("\n")
    end
  end
end
