namespace :db do
  namespace :cleanup do
    desc "Clear all execution data (loops, sessions, orders, messages, tool calls) while keeping Tasks, Models, and Providers"
    task executions: :environment do
      puts "🧹 Cleaning up execution data..."

      ActiveRecord::Base.transaction do
        # 1. Trading Data
        puts "   -> Deleting Trading Sessions, Orders and Ledger Entries..."
        LedgerEntry.delete_all
        TradingOrder.delete_all
        TradingSession.delete_all

        # 2. Benchmark Executions and Agent Benchmarks
        puts "   -> Deleting Benchmark Executions and Agent Benchmarks..."
        BenchmarkExecution.delete_all
        AgentBenchmark.delete_all

        # 3. SolidLoop Engine Data
        puts "   -> Deleting SolidLoop Data (Loops, Messages, ToolCalls, Events, Sessions)..."
        SolidLoop::ToolCall.delete_all
        SolidLoop::Message.delete_all
        SolidLoop::Event.delete_all
        SolidLoop::McpTool.delete_all
        SolidLoop::McpSession.delete_all
        SolidLoop::Loop.delete_all

        # Optional: Reset Auto-increment counters (PostgreSQL specific, but safe to skip for others)
        if ActiveRecord::Base.connection.adapter_name == 'PostgreSQL'
          puts "   -> Resetting PK sequences..."
          %w[ledger_entries trading_orders trading_sessions benchmark_executions agent_benchmarks
             solid_loop_loops solid_loop_messages solid_loop_tool_calls 
             solid_loop_events solid_loop_mcp_sessions solid_loop_mcp_tools].each do |table|
            ActiveRecord::Base.connection.reset_pk_sequence!(table)
          rescue => e
            puts "      ! Failed to reset sequence for #{table}: #{e.message}"
          end
        end
      end

      puts "✅ Cleanup complete. Tasks, Models, and Providers were preserved."
    end
  end
end
