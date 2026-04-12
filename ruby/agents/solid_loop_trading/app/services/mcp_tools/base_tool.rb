module McpTools
  class BaseTool
    attr_reader :session, :arguments

    def initialize(session, arguments = {})
      @session = session
      @arguments = arguments
    end

    def call
      raise NotImplementedError
    end
  end
end
