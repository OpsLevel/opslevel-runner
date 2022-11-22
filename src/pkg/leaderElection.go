package pkg

import (
	"context"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/util/retry"
	"math"
	"time"
)

var (
	isLeader bool
)

func RunLeaderElection(client *clientset.Clientset, runnerId, lockName, lockIdentity, lockNamespace string) {
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger := log.With().Str("worker", "leader").Logger()

	leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Lock:            lock,
		ReleaseOnCancel: true,
		LeaseDuration:   15 * time.Second,
		RenewDeadline:   10 * time.Second,
		RetryPeriod:     2 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(c context.Context) {
				isLeader = true
				logger.Info().Msgf("leader is %s", lockIdentity)
				deploymentsClient := client.AppsV1().Deployments(lockNamespace)
				for {
					time.Sleep(60 * time.Second)
					result, getErr := deploymentsClient.Get(context.TODO(), lockName, metav1.GetOptions{})
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
						result, getErr := deploymentsClient.Get(context.TODO(), lockName, metav1.GetOptions{})
						if getErr != nil {
							logger.Error().Err(getErr).Msg("Failed to get latest version of Deployment")
							return getErr
						}
						result.Spec.Replicas = &replicaCount
						_, updateErr := deploymentsClient.Update(context.TODO(), result, metav1.UpdateOptions{})
						return updateErr
					})
					if retryErr != nil {
						logger.Error().Err(retryErr).Msg("Failed to set replica count")
						continue
					}
					logger.Info().Msgf("Successfully set replica count to %v", replicaCount)
					// Not allowing this sleep interval to be configurable for now to prevent this value being set too low and
					// calling the getReplicas API endpoint too frequently
				}
			},
			OnStoppedLeading: func() {
				isLeader = false
			},
			OnNewLeader: func(currentId string) {
				if !isLeader && currentId == lockIdentity {
					logger.Info().Msgf("%s started leading!", currentId)
					return
				} else if !isLeader && currentId != lockIdentity {
					logger.Info().Msgf("leader is %s", currentId)
				}
			},
		},
	})
}

func getReplicaCount(runnerId string, currentReplicas int) (int32, error) {
	clientGQL := NewGraphClient()
	jobConcurrency := int(math.Max(float64(viper.GetInt("job-concurrency")), 1))
	runnerScale, err := clientGQL.RunnerScale(runnerId, currentReplicas, jobConcurrency)
	if err != nil {
		return 0, err
	}
	recommendedReplicaCount := float64(runnerScale.RecommendedReplicaCount)
	minReplicaCount := float64(viper.GetInt("runner-min-replicas"))
	maxReplicaCount := float64(viper.GetInt("runner-max-replicas"))
	replicaCount := math.Max(math.Min(recommendedReplicaCount, maxReplicaCount), minReplicaCount)
	return int32(replicaCount), nil
}

func GetKubernetesConfig() (*rest.Config, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides).ClientConfig()
	if err != nil {
		return nil, err
	}
	return config, nil
}
