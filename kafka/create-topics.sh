#!/usr/bin/env bash
set -euo pipefail

kafka-topics --create --if-not-exists \
  --bootstrap-server kafka:9092 \
  --topic "${KAFKA_TOPIC}" \
  --partitions 3 \
  --replication-factor 1

echo "Topic ${KAFKA_TOPIC} created or already exists"
