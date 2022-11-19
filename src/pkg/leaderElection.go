package pkg

import (
	"context"
	"github.com/rs/zerolog/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/util/retry"
	"time"
)

var (
	isLeader bool
)

func RunLeaderElection(client *clientset.Clientset, lockName, lockIdentity, lockNamespace string) {
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
				for {
					time.Sleep(60 * time.Second)
					replicaCount, err := getReplicas()
					if err != nil {
						logger.Error().Err(err).Msg("Failed to get replica count")
						continue
					}
					logger.Info().Msgf("Ideal replica count is %v", replicaCount)
					// Retry is being used below to prevent deployment update from overwriting a
					// separate and unrelated update action per client-go's recommendation:
					// https://github.com/kubernetes/client-go/blob/master/examples/create-update-delete-deployment/main.go#L117
					retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
						deploymentsClient := client.AppsV1().Deployments(lockNamespace)
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

func getReplicas() (int32, error) {
	return int32(1), nil
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
