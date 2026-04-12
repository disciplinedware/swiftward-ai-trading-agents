# This file should ensure the existence of records required to run the application in every environment (production,
# development, test). The code here should be idempotent so that it can be executed at any point in every environment.
# The data can then be loaded with the bin/rails db:seed command (or created alongside the database with db:setup).
# Trading AgentTasks — one per prompt file. Idempotent by name (the H1 title).
Dir[Rails.root.join("docs/prompts/*.md")].sort.each do |path|
  content = File.read(path).strip
  title = content.lines.first.sub(/^#\s*/, "").strip

  scenario = TradingScenario.find_or_initialize_by(name: title)
  scenario.assign_attributes(
    prompt: content,
    category: "trading",
    start_at: Time.utc(2026, 3, 1),
    end_at: Time.utc(2026, 4, 1),
    step_interval: 86400,
    initial_balance: 100_000,
    trading_pair: "BTCUSD"
  )
  scenario.save!
  puts "TradingScenario: #{scenario.name}#{" (new)" if scenario.previously_new_record?}"
end
