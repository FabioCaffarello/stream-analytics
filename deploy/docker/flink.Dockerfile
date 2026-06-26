FROM apache/flink:1.19-scala_2.12-java17

# Download connector JARs needed for Kafka source and PostgreSQL JDBC sink.
RUN curl -fL \
      "https://repo1.maven.org/maven2/org/apache/flink/flink-sql-connector-kafka/3.2.0-1.19/flink-sql-connector-kafka-3.2.0-1.19.jar" \
      -o /opt/flink/lib/flink-sql-connector-kafka.jar && \
    curl -fL \
      "https://repo1.maven.org/maven2/org/apache/flink/flink-connector-jdbc/3.2.0-1.19/flink-connector-jdbc-3.2.0-1.19.jar" \
      -o /opt/flink/lib/flink-connector-jdbc.jar && \
    curl -fL \
      "https://jdbc.postgresql.org/download/postgresql-42.7.4.jar" \
      -o /opt/flink/lib/postgresql-jdbc.jar
