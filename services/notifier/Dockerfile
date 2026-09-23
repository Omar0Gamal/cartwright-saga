FROM python:3.12-slim

RUN useradd -m -r appuser
WORKDIR /app
COPY pyproject.toml uv.lock ./
COPY src/ ./src/

RUN pip install --no-cache-dir uv && uv sync --frozen
RUN chown -R appuser /app

USER appuser
CMD ["uv", "run", "notifier"]
