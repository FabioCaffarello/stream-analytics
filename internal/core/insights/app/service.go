package app

// InsightsServiceConfig configures all use cases exposed by InsightsService.
type InsightsServiceConfig struct {
	VolumeProfile  BuildVolumeProfileConfig
	Heatmap        BuildHeatmapConfig
	JoinTrades     JoinCrossVenueTradesConfig
	OverloadDecide OverloadDecideFunc
}

// InsightsService is the entrypoint facade for the insights bounded context.
type InsightsService struct {
	VolumeProfile  *BuildVolumeProfile
	Heatmap        *BuildHeatmap
	JoinTrades     *JoinCrossVenueTrades
	OverloadPolicy *VPVREmitPolicy
}

// NewInsightsService creates all insights use cases from a single config.
func NewInsightsService(cfg InsightsServiceConfig) *InsightsService {
	return &InsightsService{
		VolumeProfile:  NewBuildVolumeProfileWithConfig(cfg.VolumeProfile),
		Heatmap:        NewBuildHeatmapWithConfig(cfg.Heatmap),
		JoinTrades:     NewJoinCrossVenueTradesWithConfig(cfg.JoinTrades),
		OverloadPolicy: NewVPVREmitPolicy(cfg.OverloadDecide),
	}
}
