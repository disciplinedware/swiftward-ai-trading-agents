require "json"

class ParseNewsJob < ApplicationJob
  include GoodJob::ActiveJobExtensions::Concurrency

  queue_as :default

  good_job_control_concurrency_with(
    key: "ParseNewsJob",
    enqueue_limit: 1
  )

  def perform
    base_dir = Rails.root.join("storage", "external", "news")
    files = Dir.glob(base_dir.join("**", "*.json")).reject do |f|
      File.basename(f).start_with?("._") || f.include?("__MACOSX")
    end

    Rails.logger.info "[ParseNewsJob] Found #{files.size} JSON files in #{base_dir}"

    files.each do |file_path|
      import_telegram_file(file_path)
    end

    Rails.logger.info "[ParseNewsJob] All imports completed"
  end

  private

  def import_telegram_file(file_path)
    data = JSON.parse(File.read(file_path, encoding: "utf-8"))
    channel_name = data["name"]
    messages = data["messages"] || []

    Rails.logger.info "[ParseNewsJob] Importing from channel: #{channel_name} (#{messages.size} messages)"

    records = []
    now = Time.current

    messages.each do |msg|
      next unless msg["type"] == "message"

      full_text = ""
      if msg["text"].is_a?(Array)
        msg["text"].each { |part| full_text += part.is_a?(Hash) ? part["text"].to_s : part.to_s }
      else
        full_text = msg["text"].to_s
      end

      next if full_text.blank?

      edited_at = msg["edited_unixtime"].present? ? Time.at(msg["edited_unixtime"].to_i) : nil

      records << {
        telegram_channel: channel_name,
        telegram_message_id: msg["id"],
        title: full_text.split("\n").first.to_s.strip.truncate(200),
        content: full_text,
        published_at: Time.at(msg["date_unixtime"].to_i),
        edited_at: edited_at,
        symbol: channel_name,
        created_at: now,
        updated_at: now
      }

      if records.size >= 500
        NewsItem.upsert_all(records, unique_by: [ :telegram_channel, :telegram_message_id ])
        records = []
      end
    end

    NewsItem.upsert_all(records, unique_by: [ :telegram_channel, :telegram_message_id ]) if records.any?

    Rails.logger.info "[ParseNewsJob] Completed import for #{channel_name}"
  rescue => e
    Rails.logger.error "[ParseNewsJob] Error processing #{file_path}: #{e.message}"
  end
end
