FROM python:3.11-slim

RUN pip install --no-cache-dir faster-whisper

# Pre-download the base model so startup is instant for the default config.
# Other models (medium, large-v3, etc.) are downloaded at runtime.
RUN python -c "from faster_whisper import WhisperModel; WhisperModel('base', device='cpu', compute_type='int8')"

COPY whisper/server.py /server.py
EXPOSE 9000
CMD ["python", "/server.py"]