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

	leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Lock:            lock,
		ReleaseOnCancel: true,
		LeaseDuration:   15 * time.Second,
		RenewDeadline:   10 * time.Second,
		RetryPeriod:     2 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(c context.Context) {
				isLeader = true
				log.Info().Msgf("Perform Migration")
				for {
					log.Info().Msgf("leader is %s", lockIdentity)
					replicaCount, err := getReplicas()
					if err != nil {
						log.Fatal().Msgf("Failed to get replica count")
					}
					log.Info().Msgf("Setting replica count to %v", replicaCount)
					retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
						deploymentsClient := client.AppsV1().Deployments(lockNamespace)
						result, getErr := deploymentsClient.Get(context.TODO(), lockName, metav1.GetOptions{})
						if getErr != nil {
							log.Fatal().Msgf("Failed to get latest version of Deployment: %v", getErr)
						}
						result.Spec.Replicas = &replicaCount
						_, updateErr := deploymentsClient.Update(context.TODO(), result, metav1.UpdateOptions{})
						return updateErr
					})
					if retryErr != nil {
						log.Fatal().Msgf("Failed to set replica count: %v", retryErr)
					}
					log.Info().Msgf("Successfully set replicas to %v", replicaCount)
					time.Sleep(60 * time.Second)
				}
			},
			OnStoppedLeading: func() {
				isLeader = false
			},
			OnNewLeader: func(currentId string) {
				if !isLeader && currentId == lockIdentity {
					log.Info().Msgf("%s started leading!", currentId)
					return
				} else if !isLeader && currentId != lockIdentity {
					log.Info().Msgf("leader is %s", currentId)
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
