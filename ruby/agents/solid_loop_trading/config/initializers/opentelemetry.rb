require 'opentelemetry/sdk'
require 'opentelemetry/instrumentation/all'
require 'opentelemetry/exporter/otlp'

# Suppress 'Instrumentation: ... was successfully installed' noise
OpenTelemetry.logger = Logger.new(STDOUT, level: Logger::WARN)

OpenTelemetry::SDK.configure do |c|
  c.service_name = ENV.fetch('OTEL_SERVICE_NAME', 'agent-ruby-solid')
  c.use_all()
end
