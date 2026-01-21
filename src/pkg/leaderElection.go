package pkg

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/opslevel/opslevel-go/v2024"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/util/retry"
)

var (
	isLeader   bool
	isLeaderMu sync.RWMutex
)

func setLeader(val bool) {
	isLeaderMu.Lock()
	defer isLeaderMu.Unlock()
	isLeader = val
}

func getLeader() bool {
	isLeaderMu.RLock()
	defer isLeaderMu.RUnlock()
	return isLeader
}

func RunLeaderElection(ctx context.Context, runnerId opslevel.ID, lockName, lockIdentity, lockNamespace string) error {
	config, err := GetKubernetesConfig()
	if err != nil {
		return err
	}
	client := clientset.NewForConfigOrDie(config)

	lock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      lockName,
			Namespace: lockNamespace,
		},
		Client: client.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: lockIdentity,
		},
	}

	logger := log.With().Str("worker", "leader").Logger()

	go leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Lock:            lock,
		ReleaseOnCancel: true,
		LeaseDuration:   15 * time.Second,
		RenewDeadline:   10 * time.Second,
		RetryPeriod:     2 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(c context.Context) {
				setLeader(true)
				logger.Info().Msgf("leader is %s", lockIdentity)
				deploymentsClient := client.AppsV1().Deployments(lockNamespace)
				// Not allowing this sleep interval to be configurable for now
				// to prevent it being set too low and causing thundering herd
				ticker := time.NewTicker(60 * time.Second)
				defer ticker.Stop()
				for {
					select {
					case <-c.Done():
						logger.Info().Msg("Leader election context cancelled, stopping scaling loop")
						return
					case <-ticker.C:
						result, getErr := deploymentsClient.Get(c, lockName, metav1.GetOptions{})
						if getErr != nil {
							logger.Error().Err(getErr).Msg("Failed to get latest version of Deployment")
							continue
						}
						replicaCount, err := getReplicaCount(runnerId, int(*result.Spec.Replicas))
						if err != nil {
							logger.Error().Err(err).Msg("Failed to get replica count")
							continue
						}
						logger.Info().Msgf("Ideal replica count is %v", replicaCount)
						// Retry is being used below to prevent deployment update from overwriting a
						// separate and unrelated update action per client-go's recommendation:
						// https://github.com/kubernetes/client-go/blob/master/examples/create-update-delete-deployment/main.go#L117
						retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
							result, getErr := deploymentsClient.Get(c, lockName, metav1.GetOptions{})
							if getErr != nil {
								logger.Error().Err(getErr).Msg("Failed to get latest version of Deployment")
								return getErr
							}
							result.Spec.Replicas = &replicaCount
							_, updateErr := deploymentsClient.Update(c, result, metav1.UpdateOptions{})
							return updateErr
						})
						if retryErr != nil {
							logger.Error().Err(retryErr).Msg("Failed to set replica count")
							continue
						}
						logger.Info().Msgf("Successfully set replica count to %v", replicaCount)
					}
				}
			},
			OnStoppedLeading: func() {
				setLeader(false)
			},
			OnNewLeader: func(currentId string) {
				if !getLeader() && currentId == lockIdentity {
					logger.Info().Msgf("%s started leading!", currentId)
					return
				} else if !getLeader() && currentId != lockIdentity {
					logger.Info().Msgf("leader is %s", currentId)
				}
			},
		},
	})
	return nil
}

func getReplicaCount(runnerId opslevel.ID, currentReplicas int) (int32, error) {
	clientGQL := NewGraphClient()
	jobConcurrency := int(math.Max(float64(viper.GetInt("job-concurrency")), 1))
	jobConcurrencyFactor := int(math.Max(float64(viper.GetInt("job-concurrency-factor")), 1))
	runnerScale, err := clientGQL.RunnerScale(runnerId, currentReplicas, jobConcurrency*jobConcurrencyFactor)
	if err != nil {
		return 0, err
	}
	recommendedReplicaCount := float64(runnerScale.RecommendedReplicaCount)
	minReplicaCount := float64(viper.GetInt("runner-min-replicas"))
	maxReplicaCount := float64(viper.GetInt("runner-max-replicas"))
	replicaCount := math.Max(math.Min(recommendedReplicaCount, maxReplicaCount), minReplicaCount)
	return int32(replicaCount), nil
}
