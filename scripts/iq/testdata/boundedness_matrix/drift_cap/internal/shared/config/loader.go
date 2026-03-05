package config
func applyDefaults(c *AppConfig) {
    c.Delivery.SessionOutboundQueueSize = 512
}
