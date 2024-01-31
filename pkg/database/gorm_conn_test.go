package database_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/stolostron/multicluster-global-hub/pkg/database"
	"github.com/stolostron/multicluster-global-hub/test/pkg/testpostgres"
	"github.com/stretchr/testify/assert"
)

var testPostgres *testpostgres.TestPostgres
var databaseConfig *database.DatabaseConfig

func TestConnectionPool(t *testing.T) {
	var err error
	testPostgres, err = testpostgres.NewTestPostgres()
	assert.Nil(t, err)

	databaseConfig = &database.DatabaseConfig{
		URL:      testPostgres.URI,
		Dialect:  database.PostgresDialect,
		PoolSize: 6,
	}
	err = database.InitGormInstance(databaseConfig)
	assert.Nil(t, err)

	err = testpostgres.InitDatabase(testPostgres.URI)
	assert.Nil(t, err)

	db := database.GetGorm()
	sqlDB, err := db.DB()
	assert.Nil(t, err)

	stats := sqlDB.Stats()
	assert.Equal(t, databaseConfig.PoolSize, stats.MaxOpenConnections)

	fmt.Println("--------------------------------------------------------")
	fmt.Println("stats.OpenConnections: ", stats.OpenConnections)
	fmt.Println("stats.MaxOpenConnections: ", stats.MaxOpenConnections)
	fmt.Println("stats.InUse: ", stats.InUse)
	fmt.Println("stats.Idle: ", stats.Idle)
}

func TestTwoInstanceCanGetLockWhenReleased(t *testing.T) {
	database.IsBackupEnabled = true

	new_gorm_db, _, err := database.NewGormConn(databaseConfig)
	assert.Nil(t, err)

	default_gorm := database.GetGorm()
	err = database.Lock(default_gorm)
	assert.Nil(t, err)
	database.Unlock(default_gorm)

	err = database.Lock(new_gorm_db)
	assert.Nil(t, err)
	database.Unlock(new_gorm_db)
}

func TestTwoInstanceCanNotGetLock(t *testing.T) {
	new_gorm_db, _, err := database.NewGormConn(databaseConfig)
	assert.Nil(t, err)

	default_gorm := database.GetGorm()

	err = database.Lock(default_gorm)
	defer database.Unlock(default_gorm)
	assert.Nil(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(time.Millisecond*800))
	defer cancel()
	timer := time.NewTimer(time.Duration(time.Millisecond * 500))
	defer database.Unlock(new_gorm_db)
	go func(ctx context.Context) {
		err = database.Lock(new_gorm_db)
		assert.Nil(t, err)
	}(ctx)

	select {
	case <-ctx.Done():
		timer.Stop()
		timer.Reset(time.Second)
		assert.Nil(t, fmt.Errorf("should not get lock"))
		return
	case <-timer.C:
		return
	}
}

func TestUnLockFail(t *testing.T) {
	new_gorm_db, _, err := database.NewGormConn(databaseConfig)
	assert.Nil(t, err)
	database.Unlock(new_gorm_db)
}
