package transformation

import (
	"clustercloner/clusters"
	transformaks "clustercloner/clusters/clouds/aks/transform"
	transformgke "clustercloner/clusters/clouds/gke/transform"
	"clustercloner/clusters/clusteraccess"
	clusterutil "clustercloner/clusters/util"
	"fmt"
	"github.com/pkg/errors"

	"github.com/urfave/cli/v2"
	"log"
)

// Transformer ...
type Transformer interface {
	CloudToHub(in *clusters.ClusterInfo) (*clusters.ClusterInfo, error)
	//	HubToCloud///
	HubToCloud(in *clusters.ClusterInfo, outputScope string) (*clusters.ClusterInfo, error)
	// LocationHubToCloud ...
	LocationHubToCloud(loc string) (string, error)
	// LocationCloudToHub ...
	LocationCloudToHub(loc string) (string, error)
}

// Clone ...
func Clone(cliCtx *cli.Context) ([]*clusters.ClusterInfo, error) {
	inputCloud := cliCtx.String("inputcloud")
	outputCloud := cliCtx.String("outputcloud")
	inputLocation := cliCtx.String("inputlocation")
	inputScope := cliCtx.String("inputscope")
	outputScope := cliCtx.String("outputscope")
	create := cliCtx.Bool("create")
	sCreate := ""
	if !create {
		sCreate = "not "
	}
	log.Printf("Will %screate target clusters", sCreate)
	if inputCloud == "" || outputCloud == "" || inputLocation == "" || inputScope == "" || outputScope == "" {
		log.Fatal("Missing flags")
	}
	return clone(inputCloud, outputCloud, inputLocation, inputScope, outputScope, create)

}

func clone(inputCloud string, outputCloud string, inputLocation string, inputScope string, outputScope string, create bool) ([]*clusters.ClusterInfo, error) {
	clusterAccessor := clusteraccess.GetClusterAccess(inputCloud)
	if clusterAccessor == nil {
		return nil, errors.New("cannot get accessor for " + inputCloud)
	}
	inputClusterInfos, err := clusterAccessor.ListClusters(inputScope, inputLocation)
	if err != nil {
		return nil, err
	}
	transformationOutput := make([]*clusters.ClusterInfo, 0)
	for _, inputClusterInfo := range inputClusterInfos {
		outputClusterInfo, err := transformCloudToCloud(inputClusterInfo, outputCloud, outputScope)
		transformationOutput = append(transformationOutput, outputClusterInfo)
		if err != nil {
			log.Printf("Error processing %v: %v", inputClusterInfo, err)
		}
	}

	if create {
		createdClusters, createdIndexes, _ := createClusters(transformationOutput)
		//replaced each ClusterInfo that was created; left the ones that were not
		var transformedClustersCreatedOrNot = make([]*clusters.ClusterInfo, len(transformationOutput))
		if len(createdClusters) != len(transformationOutput) {
			panic(fmt.Sprintf("%d!=%d", len(createdClusters), len(transformationOutput)))
		}
		for i := 0; i < len(transformationOutput); i++ {
			if clusterutil.ContainsInt(createdIndexes, i) {
				transformedClustersCreatedOrNot[i] = createdClusters[i]
			} else {
				transformedClustersCreatedOrNot[i] = transformationOutput[i]
			}
		}
		return transformedClustersCreatedOrNot, nil
	}
	log.Println("Dry run, not creating", len(transformationOutput), "target clusters")
	return transformationOutput, nil

}

// 'created' return param may be partly populated as some clusters have been successfully populated and some have not
func createClusters( /*immutable*/ createThese []*clusters.ClusterInfo) (createdClusters []*clusters.ClusterInfo, createdIndexes []int, err error) {
	createdIndexes = make([]int, 0)
	createdClusters = make([]*clusters.ClusterInfo, len(createThese))
	log.Println("Creating", len(createThese), "target clusters")
	for idx, createThis := range createThese {
		created, err := createCluster(createThis)
		if err != nil {
			log.Printf("Error creating %v: %v", createThis, err)
		} else {
			createdClusters[idx] = created //todo read the clusters back from the cloud, where feasible (but async creation may prevent that or require delay)
			createdIndexes = append(createdIndexes, idx)
		}
	}
	return createdClusters, createdIndexes, nil
}

// createCluster ...
func createCluster(createThis *clusters.ClusterInfo) (createdClusterInfo *clusters.ClusterInfo, err error) {
	var ca = clusteraccess.GetClusterAccess(createThis.Cloud)
	if ca == nil {
		return nil, errors.New("cannot creeate ClusterAccess")
	}
	created, err := ca.CreateCluster(createThis)
	if err != nil {
		log.Println("Error creating cluster", err)
	}
	return created, err
}

func transformCloudToCloud(clusterInfo *clusters.ClusterInfo, toCloud string, outputScope string) (c *clusters.ClusterInfo, err error) {

	hub, err1 := toHubFormat(clusterInfo)
	if err1 != nil || hub == nil {
		return nil, errors.Wrap(err1, "Error in transforming to CloudToHub Format")
	}
	target, err2 := fromHubFormat(hub, toCloud, outputScope)
	if err2 != nil {
		return nil, err2
	}
	if clusterInfo.Cloud == toCloud {
		// Maybe self-to-self transformation shoud not go thru hub format.
		target.Name = target.Name + "-copy"
	}
	target.GeneratedBy = clusters.TRANSFORMATION
	return target, nil
}

func toHubFormat(input *clusters.ClusterInfo) (c *clusters.ClusterInfo, err error) {
	err = nil
	var ret *clusters.ClusterInfo
	cloud := input.Cloud
	transformer := getTransformer(cloud)
	if transformer == nil {
		return nil, errors.New("cannot transform")
	}
	ret, err = transformer.CloudToHub(input)
	return ret, err
}

func getTransformer(cloud string) Transformer {
	var transformer Transformer
	switch cloud {
	case clusters.GCP:
		transformer = &transformgke.GKETransformer{}
	case clusters.AZURE:
		transformer = &transformaks.AKSTransformer{}
	case clusters.HUB:
		transformer = &IdentityTransformer{}
	default:
		transformer = nil
		log.Printf("Unknown %s", cloud)
	}
	return transformer
}
func fromHubFormat(hub *clusters.ClusterInfo, toCloud string, outputScope string) (c *clusters.ClusterInfo, err error) {
	if hub.Cloud != clusters.HUB {
		return nil, errors.New(fmt.Sprintf("Wrong Cloud %s", hub.Cloud))
	}

	var ret *clusters.ClusterInfo
	err = nil
	var transformer = getTransformer(toCloud)
	ret, err = transformer.HubToCloud(hub, outputScope)
	if err != nil {
		return nil, errors.Wrap(err, "cannot convert HubToCloud")
	}

	return ret, err
}
