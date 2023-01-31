package main

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/sanposhiho/mini-kube-scheduler/scheduler"
	"golang.org/x/xerrors"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"github.com/sanposhiho/mini-kube-scheduler/config"
	"github.com/sanposhiho/mini-kube-scheduler/k8sapiserver"
	"github.com/sanposhiho/mini-kube-scheduler/pvcontroller"
	"github.com/sanposhiho/mini-kube-scheduler/scheduler/defaultconfig"
)

// entry point.
func main() {
	if err := start(); err != nil {
		klog.Fatalf("failed with error on running scheduler: %+v", err)
	}
}

// start starts scheduler and needed k8s components.
func start() error {
	cfg, err := config.NewConfig() // 環境変数を取得(make start時に実行されるshで設定されたもの)
	if err != nil {
		return xerrors.Errorf("get config: %w", err)
	}

	// kube-apiserverを起動
	// etcdはmake start実行時に起動されているためそのURLを渡している
	restclientCfg, apiShutdown, err := k8sapiserver.StartAPIServer(cfg.EtcdURL)
	if err != nil {
		return xerrors.Errorf("start API server: %w", err)
	}
	defer apiShutdown()

	client := clientset.NewForConfigOrDie(restclientCfg) // clientsetを生成

	pvshutdown, err := pvcontroller.StartPersistentVolumeController(client)
	if err != nil {
		return xerrors.Errorf("start pv controller: %w", err)
	}
	defer pvshutdown()

	sched := scheduler.NewSchedulerService(client, restclientCfg) // schedulerのServiceインスタンスを生成(schedulerインスタンスを作ったりするためのもの)

	sc, err := defaultconfig.DefaultSchedulerConfig() // KubeSchedulerConfigurationを生成
	if err != nil {
		return xerrors.Errorf("create scheduler config")
	}

	// schedulerやInformerを作成し起動する(EventHandlerの登録もここ)
	if err := sched.StartScheduler(sc); err != nil { 
		return xerrors.Errorf("start scheduler: %w", err)
	}
	defer sched.ShutdownScheduler() // cおんてxtのcancel関数を実行してschedulerを停止

	err = scenario(client) // シナリオ実行
	if err != nil {
		return xerrors.Errorf("start scenario: %w", err)
	}

	return nil
}

// スケジューラーの動作確認を行うシミュレーションシナリオ
func scenario(client clientset.Interface) error {
	ctx := context.Background()

	// create node0 ~ node9
	for i := 0; i < 10; i++ {
		suffix := strconv.Itoa(i)
		_, err := client.CoreV1().Nodes().Create(ctx, &v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node" + suffix,
			},
		}, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("create node: %w", err)
		}
		klog.Info("scenario: node" + suffix + " created")
	}

	klog.Info("scenario: all nodes created")

	_, err := client.CoreV1().Pods("default").Create(ctx, &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod1"},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  "container1",
					Image: "k8s.gcr.io/pause:3.5",
				},
			},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create pod: %w", err)
	}

	klog.Info("scenario: pod1 created")

	// wait to schedule
	time.Sleep(4 * time.Second)

	pod, err := client.CoreV1().Pods("default").Get(ctx, "pod1", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get pod: %w", err)
	}

	klog.Info("scenario: pod1 is bound to " + pod.Spec.NodeName)

	return nil
}
