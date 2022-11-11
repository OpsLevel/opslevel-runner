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
	"time"
)

var (
	isLeader bool
)

func RunLeaderElection(client *clientset.Clientset, ctx context.Context, lockname, id, namespace string) {
	lock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      lockname,
			Namespace: namespace,
		},
		Client: client.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: id,
		},
	}
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
					log.Info().Msgf("leader is %s", id)
					log.Info().Msgf("Getting replica count...")
					time.Sleep(5 * time.Second)
				}
			},
			OnStoppedLeading: func() {
				isLeader = false
			},
			OnNewLeader: func(currentId string) {
				if !isLeader && currentId == id {
					log.Info().Msgf("%s started leading!", currentId)
					return
				} else if !isLeader && currentId != id {
					log.Info().Msgf("leader is %s", currentId)
				}
			},
		},
	})
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
