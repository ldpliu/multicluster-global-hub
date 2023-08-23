package models

import (
	"time"

	"github.com/stolostron/multicluster-global-hub/pkg/database"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type ManagedCluster struct {
	LeafHubName string         `gorm:"column:leaf_hub_name;not null"`
	ClusterID   string         `gorm:"column:cluster_id;not null"`
	Payload     datatypes.JSON `gorm:"column:payload;type:jsonb"`
	Error       string         `gorm:"column:error;not null"`
	ClusterName string         `gorm:"column:cluster_name;default:(-)"`
	CreatedAt   time.Time      `gorm:"column:created_at;default:(-)"` // https://gorm.io/docs/conventions.html#CreatedAt
	UpdatedAt   time.Time      `gorm:"column:updated_at;default:(-)"`
	DeletedAt   gorm.DeletedAt `gorm:"column:deleted_at;default:(-)"`
}

func (ManagedCluster) TableName() string {
	return "status.managed_clusters"
}

type LeafHub struct {
	LeafHubName string         `gorm:"column:leaf_hub_name;not null"`
	Payload     datatypes.JSON `gorm:"column:payload;type:jsonb"`
	ConsoleURL  string         `gorm:"column:console_url;default:(-)"`
	CreatedAt   time.Time      `gorm:"column:created_at;default:(-)"` // https://gorm.io/docs/conventions.html#CreatedAt
	UpdatedAt   time.Time      `gorm:"column:updated_at;default:(-)"`
	DeletedAt   gorm.DeletedAt `gorm:"column:deleted_at;default:(-)"`
}

func (LeafHub) TableName() string {
	return "status.leaf_hubs"
}

type StatusCompliance struct {
	PolicyID    string                    `gorm:"column:policy_id;not null"`
	ClusterName string                    `gorm:"column:cluster_name;not null"`
	LeafHubName string                    `gorm:"column:leaf_hub_name;not null"`
	Error       string                    `gorm:"column:error;not null"`
	Compliance  database.ComplianceStatus `gorm:"column:compliance;not null"`
	// ClusterID   string                    `gorm:"column:cluster_id;default:(-)"`
}

func (StatusCompliance) TableName() string {
	return "status.compliance"
}

type AggregatedCompliance struct {
	PolicyID             string `gorm:"column:policy_id;not null"`
	LeafHubName          string `gorm:"column:leaf_hub_name;not null"`
	AppliedClusters      int    `gorm:"column:applied_clusters;not null"`
	NonCompliantClusters int    `gorm:"column:non_compliant_clusters;not null"`
}

func (a AggregatedCompliance) TableName() string {
	return "status.aggregated_compliance"
}