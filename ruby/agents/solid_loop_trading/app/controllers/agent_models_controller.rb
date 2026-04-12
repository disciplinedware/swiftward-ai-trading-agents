class AgentModelsController < ApplicationController
  before_action :set_agent_model, only: %i[ show edit update destroy ]
  before_action :load_available_tools, only: %i[ new edit create update ]

  # GET /agent_models or /agent_models.json
  def index
    @agent_models = AgentModel.order(id: :desc)
  end

  # GET /agent_models/1 or /agent_models/1.json
  def show
  end

  # GET /agent_models/new
  def new
    @agent_model = AgentModel.new(agent_model_provider_id: params[:agent_model_provider_id])
    # Default to all tools enabled except TestTool
    @agent_model.tools = @available_tools.map(&:last).reject { |tn| tn == "TestTool" }
  end

  # GET /agent_models/1/edit
  def edit
  end

  # POST /agent_models or /agent_models.json
  def create
    @agent_model = AgentModel.new(agent_model_params)

    respond_to do |format|
      if @agent_model.save
        format.html { redirect_to @agent_model, notice: "Agent model was successfully created." }
        format.json { render :show, status: :created, location: @agent_model }
      else
        format.html { render :new, status: :unprocessable_content }
        format.json { render json: @agent_model.errors, status: :unprocessable_content }
      end
    end
  end

  # PATCH/PUT /agent_models/1 or /agent_models/1.json
  def update
    respond_to do |format|
      if @agent_model.update(agent_model_params)
        format.html { redirect_to @agent_model, notice: "Agent model was successfully updated.", status: :see_other }
        format.json { render :show, status: :ok, location: @agent_model }
      else
        format.html { render :edit, status: :unprocessable_content }
        format.json { render json: @agent_model.errors, status: :unprocessable_content }
      end
    end
  end

  # DELETE /agent_models/1 or /agent_models/1.json
  def destroy
    @agent_model.destroy!

    respond_to do |format|
      format.html { redirect_to agent_models_path, notice: "Agent model was successfully destroyed.", status: :see_other }
      format.json { head :no_content }
    end
  end

  private
    # Use callbacks to share common setup or constraints between actions.
    def set_agent_model
      @agent_model = AgentModel.find(params.expect(:id))
    end

    def load_available_tools
      # Eager load tools to ensure subclasses are registered
      Dir[Rails.root.join("app/tools/*.rb")].each { |f| require f }

       # Find all tool classes (excluding base classes)
       @available_tools = ApplicationTool.descendants.map do |tool_class|
        # Prefer FUNCTION_NAME constant
        func_name = if tool_class.const_defined?(:FUNCTION_NAME)
           tool_class::FUNCTION_NAME
        else
           tool_class.name
        end
        [ func_name, tool_class.name ]
      end.compact.uniq.sort_by { |name, _| name }
    end

    # Only allow a list of trusted parameters through.
    def agent_model_params
      # params.expect is strict in Rails 8. We need to permit the scalar fields and the tools array.
      # Since tools is an array of strings, we use the `tools: []` syntax.
      params.require(:agent_model).permit(:name, :llm_model_name, :agent_model_provider_id, :description,
                                         :price_per_input_token, :price_per_output_token, :price_per_cached_token,
                                         tools: [])
    end
end
