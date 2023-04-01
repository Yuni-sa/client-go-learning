package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/itchyny/gojq"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

func main() {
	// Load Kubernetes configuration from the default location ($HOME/.kube/config)
	config, err := clientcmd.BuildConfigFromFlags("", filepath.Join(homedir.HomeDir(), ".kube", "config"))
	if err != nil {
		panic(err.Error())
	}

	// Create a Kubernetes clientset and dynamic client
	//clientset, err := kubernetes.NewForConfig(config)
	//if err != nil {
	//	panic(err.Error())
	//}
	dynamicClient := dynamic.NewForConfigOrDie(config)

	// Create a new scheme and add the necessary types
	scheme := runtime.NewScheme()
	metav1.AddToGroupVersion(scheme, metav1.SchemeGroupVersion)

	// Create a new codec object that can handle the necessary types
	codecs := serializer.NewCodecFactory(scheme)

	// Create a new YAML decoder using the scheme and codec object
	decoder := codecs.UniversalDeserializer()

	// Read the manifest file
	manifestPath := "my-deployment.yaml"
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		panic(err.Error())
	}

	// Decode the manifest into a runtime.Object
	manifestObj := &unstructured.Unstructured{}
	if _, _, err := decoder.Decode(manifestBytes, nil, manifestObj); err != nil {
		panic(err.Error())
	}

	// Get the group, version, and kind from the manifest
	gvk := manifestObj.GroupVersionKind()
	namespace := manifestObj.GetNamespace()

	//log.Println(namespace)

	// If no version is specified, use the default values
	if gvk.Version == "" {
		gvk.Version = "v1"
	}
	// If no namespace is specified, use the default namespace
	if namespace == "" {
		namespace = "default"
	}

	// Get the resource from the dynamic client
	resource := dynamicClient.Resource(gvk.GroupVersion().WithResource(strings.ToLower(gvk.Kind) + "s")).Namespace(namespace)
	//log.Println(resource)

	// Apply the manifest
	_, err = resource.Create(context.Background(), manifestObj, metav1.CreateOptions{})
	if err != nil {
		log.Println(err.Error())
	} else {
		fmt.Printf("Manifest %q applied successfully.\n", manifestPath)
	}

	_, err = resource.Get(context.Background(), manifestObj.GetName(), metav1.GetOptions{})
	if errors.IsNotFound(err) {
		fmt.Printf("Pod %q not found in default namespace\n", manifestObj.GetName())
	} else if statusError, isStatus := err.(*errors.StatusError); isStatus {
		fmt.Printf("Error getting pod %v\n", statusError.ErrStatus.Message)
	} else if err != nil {
		panic(err.Error())
	} else {
		fmt.Printf("Found %q pod in default namespace\n", manifestObj.GetName())
	}

	//list, err := resource.List(context.Background(), metav1.ListOptions{FieldSelector: "metadata.name=golang-auth-deployment"})

	// For every pod in the default namespace print the first container image
	list, err := resource.List(context.Background(), metav1.ListOptions{})
	if err != nil {
		log.Println(err.Error())
	} else {
		println(list.Items)
		for _, item := range list.Items {
			// Extract the containers slice using unstructured.NestedSlice
			containers, found, err := unstructured.NestedSlice(item.Object, "spec", "template", "spec", "containers")
			if err != nil {
				// Handle the error
				fmt.Printf("Error extracting containers slice: %v\n", err)
				return
			}

			if !found {
				// Handle the case where the field is not found
				fmt.Printf("Containers slice not found\n")
				return
			}

			// Get the first container in the slice
			firstContainer, ok := containers[0].(map[string]interface{})
			if !ok {
				// Handle the case where the first item in the slice is not a map
				fmt.Printf("First item in containers slice is not a map\n")
				return
			}

			// Extract the container image name from the first container
			imageName, found, err := unstructured.NestedString(firstContainer, "image")
			if err != nil {
				// Handle the error
				fmt.Printf("Error extracting container image name: %v\n", err)
				return
			}

			if !found {
				// Handle the case where the field is not found
				fmt.Printf("Container image name field not found\n")
				return
			}

			// Print the image name
			fmt.Println(imageName)

		}
	}

	fmt.Printf("\n")
	query := ".metadata.labels[\"app\"] == \"ginx\""
	items, err := GetResourcesByJq(dynamicClient, context.Background(), "apps", "v1", "deployments", namespace, query)
	if err != nil {
		fmt.Println(err)
	} else {
		for _, item := range items {
			fmt.Printf("%+v\n", item)
		}
	}

	//err = resource.Delete(context.Background(), manifestObj.GetName(), metav1.DeleteOptions{})
	//if err != nil {
	//	log.Println(err.Error())
	//} else {
	//	fmt.Printf("Manifest %q deleted successfully.\n", manifestPath)
	//}

	_, err = resource.Get(context.Background(), manifestObj.GetName(), metav1.GetOptions{})
	if errors.IsNotFound(err) {
		fmt.Printf("Pod %q not found in default namespace\n", manifestObj.GetName())
	} else if statusError, isStatus := err.(*errors.StatusError); isStatus {
		fmt.Printf("Error getting pod %v\n", statusError.ErrStatus.Message)
	} else if err != nil {
		panic(err.Error())
	} else {
		fmt.Printf("Found %q pod in default namespace\n", manifestObj.GetName())
	}

}

func GetResourcesByJq(dynamic dynamic.Interface, ctx context.Context, group string,
	version string, resource string, namespace string, jq string) (
	[]unstructured.Unstructured, error) {

	resources := make([]unstructured.Unstructured, 0)

	query, err := gojq.Parse(jq)
	if err != nil {
		return nil, err
	}

	items, err := GetResourcesDynamically(dynamic, ctx, group, version, resource, namespace)
	if err != nil {
		return nil, err
	}

	for _, item := range items {
		// Convert object to raw JSON
		var rawJson interface{}
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(item.Object, &rawJson)
		if err != nil {
			return nil, err
		}

		// Evaluate jq against JSON
		iter := query.Run(rawJson)
		for {
			result, ok := iter.Next()
			if !ok {
				break
			}
			if err, ok := result.(error); ok {
				if err != nil {
					return nil, err
				}
			} else {
				boolResult, ok := result.(bool)
				if !ok {
					fmt.Println("Query returned non-boolean value")
				} else if boolResult {
					resources = append(resources, item)
				}
			}
		}
	}
	return resources, nil
}

func GetResourcesDynamically(dynamic dynamic.Interface, ctx context.Context,
	group string, version string, resource string, namespace string) (
	[]unstructured.Unstructured, error) {

	resourceId := schema.GroupVersionResource{
		Group:    group,
		Version:  version,
		Resource: resource,
	}
	list, err := dynamic.Resource(resourceId).Namespace(namespace).
		List(ctx, metav1.ListOptions{})

	if err != nil {
		return nil, err
	}

	return list.Items, nil
}
