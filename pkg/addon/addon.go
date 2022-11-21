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

func (a *Addon) getPodLog(ctx context.Context, pod string) error {
	request := a.clientset.CoreV1().Pods(a.sourceNamespace).GetLogs(pod, &corev1.PodLogOptions{Container: "sealed-secrets-controller", Follow: true})
	stream, err := request.Stream(ctx)
	if err != nil {
		return err
	}
	a.streams[pod] = stream

	return nil
}

func (a *Addon) streamPodLog(ctx context.Context, pod string) error {
	log.Printf("scanning log stream of %v", pod)
	regexfilter, err := regexp.Compile("SealedSecret unsealed successfully")
	if err != nil {
		return err
	}
	scanner := bufio.NewScanner(a.streams[pod])
	for scanner.Scan() {
		logmsg := scanner.Text()
		if bln := regexfilter.MatchString(logmsg); bln {
			trimleftall := logmsg[strings.Index(logmsg, "{")+1:]
			trimrightall := trimleftall[:strings.Index(trimleftall, "}")]
			eventattr := make(map[string]string)
			for _, e := range strings.Split(trimrightall, ", ") {
				kv := strings.Split(e, ":")
				eventattr[kv[0]] = strings.Trim(kv[1], "\"")
			}
			a.copySecrets(ctx, eventattr["Name"])
		}
	}

	log.Printf("log stream of %v closed.", pod)
	delete(a.streams, pod)
	if ctx.Err() == nil {
		log.Printf("start Procedure again.")
		*a.state <- "running"
	}

	return nil
}

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
	oldCopyExists := true
	oldCopy, err := a.clientset.CoreV1().Secrets(dstns).Get(ctx, secret, metav1.GetOptions{})
	if err != nil {
		oldCopyExists = false
	}
	newCopyData := confv1.Secret(origin.Name, dstns)
	newCopyData.WithLabels(origin.Labels)
	newCopyData.WithData(origin.Data)
	newCopy, err := a.clientset.CoreV1().Secrets(dstns).Apply(ctx, newCopyData, metav1.ApplyOptions{FieldManager: "sealed-secrets-addon"})
	if err != nil {
		log.Printf("tried to copy but %v", err)
		return err
	}

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

		time.Sleep((time.Minute))
	}
}

func (a *Addon) handleStreams(ctx context.Context) {
	for pod, stream := range a.streams {
		log.Printf("closing log stream of %v", pod)
		if err := stream.Close(); err != nil {
			log.Printf("error on closing stream, %v", err)
			continue
		}
	}

	time.Sleep(time.Second * 3)
}
