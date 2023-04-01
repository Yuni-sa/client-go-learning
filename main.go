package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

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
	yamlDocs := strings.Split(string(manifestBytes), "---")
	if err != nil {
		panic(err.Error())
	}
	for _, yamlDoc := range yamlDocs {
		if len(strings.TrimSpace(yamlDoc)) == 0 {
			continue // Skip empty documents
		}
		// Decode the manifest into a runtime.Object
		manifestObj := &unstructured.Unstructured{}
		if _, _, err := decoder.Decode([]byte(yamlDoc), nil, manifestObj); err != nil {
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

		if gvk.Kind == "Deployment" || gvk.Kind == "Pod" {
			fmt.Println(GetContainerImage(resource, context.Background()))
		}

		// Delete the manifest
		err = resource.Delete(context.Background(), manifestObj.GetName(), metav1.DeleteOptions{})
		if err != nil {
			log.Println(err.Error())
		} else {
			fmt.Printf("Manifest %q deleted successfully.\n", manifestPath)
		}

		GetResources(resource, context.Background(), manifestObj, gvk)

	}

}

// For every pod of the object in the default namespace print the first container image
func GetContainerImage(resource dynamic.ResourceInterface, ctx context.Context) string {
	//list, err := resource.List(context.Background(), metav1.ListOptions{FieldSelector: "metadata.name=golang-auth-deployment"})

	list, err := resource.List(ctx, metav1.ListOptions{})
	if err != nil {
		return (fmt.Sprintf(err.Error()))
	} else {
		for _, item := range list.Items {
			// Extract the containers slice using unstructured.NestedSlice
			containers, found, err := unstructured.NestedSlice(item.Object, "spec", "template", "spec", "containers")
			if err != nil {
				// Handle the error
				return (fmt.Sprintf("Error extracting containers slice: %v\n", err))

			}

			if !found {
				// Handle the case where the field is not found
				return (fmt.Sprintf("Containers slice not found\n"))
			}

			// Get the first container in the slice
			firstContainer, ok := containers[0].(map[string]interface{})
			if !ok {
				// Handle the case where the first item in the slice is not a map
				return (fmt.Sprintf("First item in containers slice is not a map\n"))
			}

			// Extract the container image name from the first container
			imageName, found, err := unstructured.NestedString(firstContainer, "image")
			if err != nil {
				// Handle the error
				return (fmt.Sprintf("Error extracting container image name: %v\n", err))
			}

			if !found {
				// Handle the case where the field is not found
				return (fmt.Sprintf("Container image name field not found\n"))
			}

			// Print the image name
			return (fmt.Sprintf(imageName))

		}
	}
	return ""
}

// Get the resources
func GetResources(resource dynamic.ResourceInterface, ctx context.Context, manifestObj *unstructured.Unstructured, gvk schema.GroupVersionKind) {
	_, err := resource.Get(ctx, manifestObj.GetName(), metav1.GetOptions{})
	if errors.IsNotFound(err) {
		fmt.Printf("%v %q not found in default namespace\n", gvk.Kind, manifestObj.GetName())
	} else if statusError, isStatus := err.(*errors.StatusError); isStatus {
		fmt.Printf("Error getting %v %v\n", gvk.Kind, statusError.ErrStatus.Message)
	} else if err != nil {
		panic(err.Error())
	} else {
		fmt.Printf("Found %q %v in default namespace\n", manifestObj.GetName(), gvk.Kind)
	}
}
