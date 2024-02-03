package addon

import (
	"bufio"
	"context"
	"io"
	"log"
	"os"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	flag "github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	confv1 "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type Addon struct {
	state                *chan string
	clientset            *kubernetes.Clientset
	streams              map[string]io.ReadCloser
	controllerName       string
	sourceNamespace      string
	destinationNamespace string
}

// factory function to create new addon instance.
func NewAddon(fs *flag.FlagSet) *Addon {
	state := make(chan string, 1)
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Printf("error on getting k8s config, %v", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Printf("error on getting k8s client, %v", err)
	}
	streams := make(map[string]io.ReadCloser)
	cntr := fs.String("cntr", "sealed-secrets-controller", "specify the controller name of sealed secrets")
	srcns := fs.String("srcns", "sealedsecrets", "specify source namespace where the sealed-secrets-conroller runs.")
	dstns := fs.String("dstns", "default", "specify destination namespace where the copied secrets move to")
	if err := fs.Parse(os.Args); err != nil {
		log.Printf("error on parsing args, %v", err)
		return nil
	}

	return &Addon{
		state:                &state,
		clientset:            clientset,
		streams:              streams,
		controllerName:       *cntr,
		sourceNamespace:      *srcns,
		destinationNamespace: *dstns,
	}
}

// main loop of addon instance.
// it starts main procedure then wait for next signal through channel.
func (a *Addon) Run(ctx context.Context) {
	log.Printf("started sealed-secrets-addon.")
	*a.state <- "running"

	var once sync.Once
outer:
	for {
		select {
		case <-*a.state:
			log.Printf("started procedure.")
			if err := a.startProcedure(ctx); err != nil {
				log.Printf("error on starting loop, %v", err)
				break outer
			}
			// to prevent from executing validator repeatedly
			go once.Do(func() {
				a.validateSecrets(ctx)
			})
		case <-ctx.Done():
			a.handleStreams(ctx)
			log.Printf("terminated sealed-secrets-addon.")
			break outer
		}
	}
}

// this procedure executes functions below sequentially,
// 1. getPods
// 2. getPodLog
// 3. streamPodLog
func (a *Addon) startProcedure(ctx context.Context) error {
	deploy, err := a.clientset.AppsV1().Deployments(a.sourceNamespace).Get(ctx, a.controllerName, metav1.GetOptions{})
	if err != nil {
		log.Printf("error on getting deployment: %v", err.Error())
		return err
	}
	replicas := *deploy.Spec.Replicas
	log.Printf("current number of replicas: %v", replicas)

	regexfilter, _ := regexp.Compile(a.controllerName)
	for ctx.Err() == nil {
		pods := a.getPods(ctx, regexfilter)
		if len(pods) == int(replicas) {
			log.Printf("found %v sealed secrets pod(s).", len(pods))
			for _, pod := range pods {
				if _, exist := a.streams[pod]; exist {
					continue
				}
				if err := a.getPodLog(ctx, pod); err != nil {
					log.Printf("error on getting pod log, %v", err)
					continue
				}
				go a.streamPodLog(ctx, pod)
			}
			break
		}

		log.Printf("searching for sealed-secrets-controller pods..")
		time.Sleep(time.Second * 1)
	}

	return nil
}

// this fucntion gets pods of targeted sealed-secrets-controller.
func (a *Addon) getPods(ctx context.Context, regexfilter *regexp.Regexp) []string {
	pods, err := a.clientset.CoreV1().Pods(a.sourceNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Printf("error on getting pod list: %v", err.Error())
		return nil
	}

	found := make([]string, 0)
	for _, pod := range pods.Items {
		if regexfilter.MatchString(pod.Name) {
			found = append(found, pod.Name)
		}
	}

	return found
}

// this fucntion gets pod's log as stream from the the pods.
func (a *Addon) getPodLog(ctx context.Context, pod string) error {
	request := a.clientset.CoreV1().Pods(a.sourceNamespace).GetLogs(pod, &corev1.PodLogOptions{Container: "sealed-secrets-controller", Follow: true})
	stream, err := request.Stream(ctx)
	if err != nil {
		return err
	}
	a.streams[pod] = stream

	return nil
}

// this fucntion keeps scanning log streams of the the pods.
// when decrypted secret found, it executes copySecrets function.
func (a *Addon) streamPodLog(ctx context.Context, pod string) error {
	log.Printf("scanning log stream of %v", pod)
	regexfilter, err := regexp.Compile("SealedSecret unsealed successfully")
	if err != nil {
		return err
	}
	scanner := bufio.NewScanner(a.streams[pod])
	for scanner.Scan() {
		logmsg := scanner.Text()
		if regexfilter.MatchString(logmsg) {
			eventattr := make(map[string]string)
			for _, e := range trimSplit(logmsg) {
				kv := strings.Split(e, ":")
				eventattr[kv[0]] = strings.Trim(kv[1], "\"")
			}
			a.copySecrets(ctx, eventattr["Name"])
		}
	}

	log.Printf("log stream of %v closed.", pod)

	// to clean up the log stream of deleted pod.
	// pod deletion can happen when rolling update or terminating addon instance.
	delete(a.streams, pod)
	if ctx.Err() == nil {
		log.Printf("start Procedure again.")
		*a.state <- "running"
	}

	return nil
}

// this fucntion trims and splits log messages to convert it as map type.
func trimSplit(logmsg string) []string {
	trimmedLeftAll := logmsg[strings.Index(logmsg, "{")+1:]
	trimmedRightAll := trimmedLeftAll[:strings.Index(trimmedLeftAll, "}")]

	return strings.Split(trimmedRightAll, ", ")
}

// this fucntion copies decrypted secret to make new independent secret.
func (a *Addon) copySecrets(ctx context.Context, secret string) error {
	origin, err := a.clientset.CoreV1().Secrets(a.sourceNamespace).Get(ctx, secret, metav1.GetOptions{})
	if err != nil {
		log.Printf("error on getting secret, %v", err)
		return err
	}

	dstns := a.destinationNamespace
	if _, exist := origin.Labels["TargetNamespace"]; exist {
		dstns = origin.Labels["TargetNamespace"]
	}

	// it checks whether create new one or update already made one
	// and provide copy result in log messages to help manage the addon.
	oldCopyExists := true
	// to get previous copied secret info.
	oldCopy, err := a.clientset.CoreV1().Secrets(dstns).Get(ctx, secret, metav1.GetOptions{})
	if err != nil {
		oldCopyExists = false
	}
	newCopyData := confv1.Secret(origin.Name, dstns)
	newCopyData.WithLabels(origin.Labels)
	newCopyData.WithData(origin.Data)
	// to make new copied secret and get its info.
	newCopy, err := a.clientset.CoreV1().Secrets(dstns).Apply(ctx, newCopyData, metav1.ApplyOptions{FieldManager: "sealed-secrets-addon"})
	if err != nil {
		log.Printf("tried to copy but %v", err)
		return err
	}
	// to get copy result.
	result := "created"
	if oldCopyExists {
		result = "updated"
		if reflect.DeepEqual(oldCopy.Data, newCopy.Data) && reflect.DeepEqual(oldCopy.Labels, newCopy.Labels) {
			result = "unchanged"
			return nil
		}
	}
	log.Printf("copied secret %v in %v, result: %v", newCopy.Name, newCopy.Namespace, result)

	return nil
}

// this fucntion validates all copied secrets to ensure data integration
func (a *Addon) validateSecrets(ctx context.Context) {
	for ctx.Err() == nil {
		secrets, err := a.clientset.CoreV1().Secrets(a.sourceNamespace).List(ctx, metav1.ListOptions{FieldSelector: "type=opaque"})
		if err != nil {
			log.Printf("error on getting original secrets in %v, %v", a.sourceNamespace, err)
			continue
		}
		for _, origin := range secrets.Items {
			dstns := a.destinationNamespace
			if _, exist := origin.Labels["TargetNamespace"]; exist {
				dstns = origin.Labels["TargetNamespace"]
			}
			copied, err := a.clientset.CoreV1().Secrets(dstns).Get(ctx, origin.Name, metav1.GetOptions{})
			if err != nil {
				log.Printf("error on getting copied secret in %v, %v", dstns, err)
				continue
			}
			if !reflect.DeepEqual(origin.Data, copied.Data) || !reflect.DeepEqual(origin.Labels, copied.Labels) {
				log.Printf("copied secret doens't have the same data with the original !! - %v in %v", copied.Name, copied.Namespace)
			}
		}

		// to make interval period.
		time.Sleep((time.Minute))
	}
}

// this function is responsible for closing log streams when SIGTERM received.
func (a *Addon) handleStreams(ctx context.Context) {
	for pod, stream := range a.streams {
		log.Printf("closing log stream of %v", pod)
		if err := stream.Close(); err != nil {
			log.Printf("error on closing stream, %v", err)
			continue
		}
	}

	// to wait for closing streams completely.
	time.Sleep(time.Second * 3)
}
