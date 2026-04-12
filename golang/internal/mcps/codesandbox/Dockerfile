FROM python:3.12-slim
RUN pip install --no-cache-dir \
    pandas numpy scipy \
    pandas-ta scikit-learn statsmodels plotly requests
WORKDIR /app
COPY repl.py .
CMD ["python", "repl.py"]
