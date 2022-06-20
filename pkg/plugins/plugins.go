package plugins

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog"
	framework "k8s.io/kubernetes/pkg/scheduler/framework/v1alpha1"
	"k8s.io/kubernetes/pkg/scheduler/nodeinfo"
)

// 插件名称
const Name = "caihcloud-real-node-load-plugin"
const AnnotationPrefix = "caih.com/scheduler_"

type RealNodeLoad struct {
	handle             framework.FrameworkHandle
	realNodeLoadScorer realNodeLoadScorer
}

type resourceToMap map[v1.ResourceName]MetricsInfo

// realNodeLoadScorer contains information to calculate resource allocation score.
type realNodeLoadScorer struct {
	Name   string
	scorer func(requested resourceToMap) float64
}

type MetricsInfo struct {
	Timestamp int64
	Interval  int64
	Value     float64
	Threshold float64
	Weight    float64
}

func (rnl *RealNodeLoad) Name() string {
	return Name
}

func (rnl *RealNodeLoad) PreFilter(ctx context.Context, pc *framework.CycleState, pod *v1.Pod) *framework.Status {
	klog.V(3).Infof("prefilter pod: %v", pod.Name)
	return framework.NewStatus(framework.Success, "")
}

func (rnl *RealNodeLoad) PreFilterExtensions() framework.PreFilterExtensions {
	return nil
}

func (rnl *RealNodeLoad) Filter(ctx context.Context, pc *framework.CycleState, pod *v1.Pod, nodeInfo *nodeinfo.NodeInfo) *framework.Status {
	nodeAnnotations := nodeInfo.Node().GetAnnotations()
	klog.V(3).Infof("filter node: %v(%v)", nodeInfo.Node().GetName(), pod.Name)

	//如果节点的注释没有写入节点负载状况或label指标参数超时，让节点通过
	for k, v := range nodeAnnotations {
		if !strings.HasPrefix(k, AnnotationPrefix) {
			continue
		}
		metric, err := parseAnnotationValue(v)
		if err != nil {
			klog.Warningf("node: %v, parse annotation %v:%v invalid: %v", nodeInfo.Node().GetName(), k, v, err.Error())
			return framework.NewStatus(framework.Success, "")
		}
		if metric.Threshold == 0 || metric.Threshold == 1 {
			continue
		}
		if metric.Value > metric.Threshold {
			klog.Errorf(`exit Filter stage filter node %v with %v, value/threshold: %v%%/%v%%`,
				nodeInfo.Node().GetName(), k, metric.Value, metric.Threshold)
			return framework.NewStatus(framework.UnschedulableAndUnresolvable, "node load is too high")
		}
		continue
	}

	return framework.NewStatus(framework.Success, "")
}

func (rnl *RealNodeLoad) PreBind(ctx context.Context, pc *framework.CycleState, pod *v1.Pod, nodeName string) *framework.Status {
	if nodeInfo, err := rnl.handle.SharedInformerFactory().Core().V1().Nodes().Lister().Get(nodeName); err != nil {
		klog.Errorf("PreBind pod: %v, node info: %+v err: %v", pod.Name, nodeInfo, err.Error())
		return framework.NewStatus(framework.Error, fmt.Sprintf("prebind get node info error: %+v", nodeName))
	} else {
		klog.V(7).Infof("prebind pod: %v, node info: %+v", pod.Name, nodeInfo)
		return framework.NewStatus(framework.Success, "")
	}
}

func (rnl *RealNodeLoad) ScoreExtensions() framework.ScoreExtensions {
	return nil
}

func (rnl *RealNodeLoad) NormalizeScore(ctx context.Context, state *framework.CycleState, p *v1.Pod, scores framework.NodeScoreList) *framework.Status {
	// 在调度器计算节点的最终排名之前修正分数
	for _, score := range scores {
		klog.V(3).Infof("Name %+v score: %+v", score.Name, score.Score)
	}
	return nil
}

func (rnl *RealNodeLoad) Score(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeName string) (int64, *framework.Status) {
	nodeInfo, err := rnl.handle.SnapshotSharedLister().NodeInfos().Get(nodeName)
	if err != nil {
		klog.Errorf("score node %v err: %v", nodeInfo.Node().GetName(), err.Error())
		return 0, framework.NewStatus(framework.Error, fmt.Sprintf("getting node %q from Snapshot: %v", nodeName, err))
	}
	return rnl.score(pod, nodeInfo)
}

// Score invoked at the score extension point.
func (rnl *RealNodeLoad) score(pod *v1.Pod, nodeInfo *nodeinfo.NodeInfo) (int64, *framework.Status) {
	node := nodeInfo.Node()
	nodeAnnotations := node.GetAnnotations()
	klog.V(7).Infof("get node %+v annotation: %+v", node.GetName(), nodeAnnotations)

	nodeLoad := resourceToMap{}
	for k, v := range nodeAnnotations {
		var err error
		if !strings.HasPrefix(k, AnnotationPrefix) {
			continue
		}
		metric, err := parseAnnotationValue(v)
		if err != nil {
			klog.Warningf("node: %v, parse annotation %v:%v invalid: %v", nodeInfo.Node().GetName(), k, v, err.Error())
			metric.Value = 0
			nodeLoad[v1.ResourceName(k)] = metric
			continue
		}

		nodeLoad[v1.ResourceName(k)] = metric
	}

	score := rnl.realNodeLoadScorer.scorer(nodeLoad)
	klog.V(3).Infof("node %+v score: %+v", node.GetName(), score)

	return int64(score), nil
}

// Bind skip this plugin because system scheduler has default bind plugin
func (rnl *RealNodeLoad) Bind(ctx context.Context, state *framework.CycleState, p *v1.Pod, nodeName string) *framework.Status {
	klog.V(3).Infof("skip RealNodeLoad plugin Bind stage Pod: %s, Node: %s", p.GetName(), nodeName)
	return framework.NewStatus(framework.Skip, "")
}

func New(configuration *runtime.Unknown, f framework.FrameworkHandle) (framework.Plugin, error) {
	return &RealNodeLoad{
		handle: f,
		realNodeLoadScorer: realNodeLoadScorer{
			Name:   Name,
			scorer: realNodeLoadScorerFunc(),
		},
	}, nil
}

func realNodeLoadScorerFunc() func(resToMap resourceToMap) float64 {
	return func(resToMap resourceToMap) float64 {
		var nodeScore, weightSum float64
		for k, m := range resToMap {
			resourceScore := realNodeLoadScore(m.Value)
			nodeScore += resourceScore * 100 * m.Weight
			klog.V(7).Infof("resource: %s, weight: %f, resourceScore:%f", k, m.Weight, resourceScore)
			weightSum += m.Weight
		}

		return nodeScore / weightSum
	}
}

// The more unused resources the higher the score is.
func realNodeLoadScore(usage float64) float64 {
	if usage == 0 {
		return 0
	}

	if usage > 1 {
		return 0
	}

	return 1 - usage
}

func parseAnnotationValue(value string) (MetricsInfo, error) {
	res := MetricsInfo{}
	annoValue := strings.Split(value, ":")
	if len(annoValue) != 5 {
		return res, errors.New("value format error")
	}
	timestamp, err := strconv.ParseInt(annoValue[0], 10, 64)
	if err != nil {
		return res, err
	}
	interval, err := strconv.ParseInt(annoValue[1], 10, 64)
	if err != nil {
		return res, err
	}
	mValue, err := strconv.ParseFloat(annoValue[2], 64)
	if err != nil {
		return res, err
	}
	threshold, err := strconv.ParseFloat(annoValue[3], 64)
	if err != nil {
		return res, err
	}
	weight, err := strconv.ParseFloat(annoValue[4], 64)
	if err != nil {
		return res, err
	}
	if time.Now().Unix()-timestamp > 2*interval {
		return res, errors.New("value invalid, process timeout")
	}

	res = MetricsInfo{
		Timestamp: timestamp,
		Interval:  interval,
		Value:     mValue,
		Threshold: threshold,
		Weight:    weight,
	}
	return res, nil
}
