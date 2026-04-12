namespace :news do
  desc "Импортировать все новости из папки storage/external/news/**/*.json"
  task import_all: :environment do
    base_dir = Rails.root.join("storage", "external", "news")
    files = Dir.glob(base_dir.join("**", "*.json")).reject do |f| 
      File.basename(f).start_with?("._") || f.include?("__MACOSX")
    end

    if files.empty?
      puts "No valid JSON files found in #{base_dir}"
      next
    end

    puts "Found #{files.size} valid files to import."
    files.each do |file_path|
      # We use Rake::Task[...].invoke or just call the logic
      # Using Task[].execute to allow multiple calls in one run
      Rake::Task["news:import_telegram"].execute(file_path: file_path)
    end
    puts "All imports completed."
  end

  desc "Импорт новостей из JSON-экспорта Telegram (Fast Mass Import)"
  task :import_telegram, [:file_path] => :environment do |t, args|
    file_path = args[:file_path]
    unless file_path && File.exist?(file_path)
      puts "Error: Please provide a valid file path."
      exit 1
    end

    puts "--- Processing: #{File.basename(file_path)} ---"
    
    begin
      data = JSON.parse(File.read(file_path, encoding: 'utf-8'))
      channel_name = data['name']
      messages = data['messages'] || []
      
      puts "Importing from channel: #{channel_name}..."
      
      records = []
      now = Time.current

      messages.each do |msg|
        next unless msg['type'] == 'message'
        
        full_text = ""
        if msg['text'].is_a?(Array)
          msg['text'].each { |part| full_text += part.is_a?(Hash) ? part['text'].to_s : part.to_s }
        else
          full_text = msg['text'].to_s
        end

        next if full_text.blank?

        edited_at = msg['edited_unixtime'].present? ? Time.at(msg['edited_unixtime'].to_i) : nil

        records << {
          telegram_channel: channel_name,
          telegram_message_id: msg['id'],
          title: full_text.split("\n").first.to_s.strip.truncate(200),
          content: full_text,
          published_at: Time.at(msg['date_unixtime'].to_i),
          edited_at: edited_at,
          symbol: channel_name,
          created_at: now,
          updated_at: now
        }

        # Insert in batches of 500
        if records.size >= 500
          NewsItem.upsert_all(records, unique_by: [:telegram_channel, :telegram_message_id])
          records = []
        end
      end

      # Final batch
      if records.any?
        NewsItem.upsert_all(records, unique_by: [:telegram_channel, :telegram_message_id])
      end

      puts "Done! Mass import finished for #{channel_name}."
    rescue => e
      puts "Error processing #{file_path}: #{e.message}"
    end
  end
end
