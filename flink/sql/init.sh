#!/usr/bin/env bash
set -euo pipefail

SQL_DIR="/sql"
FLINK_SQL_CLIENT="/opt/flink/bin/sql-client.sh"
JOBMANAGER_REST="http://flink-jobmanager:8081"
COMBINED_SQL="/tmp/flink-init-combined.sql"

# Wait for Flink REST API to be ready.
echo "flink-init: waiting for Flink JobManager at ${JOBMANAGER_REST}"
for i in $(seq 1 60); do
    if curl -sf "${JOBMANAGER_REST}/overview" > /dev/null 2>&1; then
        echo "flink-init: JobManager is ready"
        break
    fi
    echo "flink-init: attempt ${i}/60 — not ready yet, sleeping 5s"
    sleep 5
done

# Concatenate all SQL files into a single script so DDL (CREATE TABLE)
# and DML (INSERT INTO) share the same session and the tables are visible.
echo "flink-init: building combined SQL script"
# Prepend restart strategy so jobs recover from transient source failures.
cat > "${COMBINED_SQL}" << 'EOF'
SET 'restart-strategy' = 'fixed-delay';
SET 'restart-strategy.fixed-delay.attempts' = '10';
SET 'restart-strategy.fixed-delay.delay' = '10s';

EOF
for sql_file in "${SQL_DIR}"/0*.sql; do
    echo "-- === ${sql_file} ===" >> "${COMBINED_SQL}"
    cat "${sql_file}" >> "${COMBINED_SQL}"
    echo "" >> "${COMBINED_SQL}"
done

# Substitute env-var placeholders that Flink SQL cannot resolve at runtime.
sed -i \
  -e "s/\${TIMESCALE_USER}/${TIMESCALE_USER}/g" \
  -e "s/\${TIMESCALE_PASSWORD}/${TIMESCALE_PASSWORD}/g" \
  "${COMBINED_SQL}"

echo "flink-init: submitting combined SQL ($(wc -l < "${COMBINED_SQL}") lines)"

"${FLINK_SQL_CLIENT}" \
    -Drest.address=flink-jobmanager \
    -Drest.port=8081 \
    --jar /opt/flink/lib/flink-sql-connector-kafka.jar \
    --jar /opt/flink/lib/flink-connector-jdbc.jar \
    --jar /opt/flink/lib/postgresql-jdbc.jar \
    -f "${COMBINED_SQL}"

echo "flink-init: all SQL jobs submitted successfully"
