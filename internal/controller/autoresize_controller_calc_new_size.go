package controller

import (
	"k8s.io/apimachinery/pkg/api/resource"
)

// helper to calculate the new size a persistent volume should be.

func calculateNewSize(current resource.Quantity, increasePercent int) resource.Quantity {
	// set the current size, to bytes
	currentSize := current.Value()
	floatBytes := float64(currentSize)
	newBytes := floatBytes * (1 + float64(increasePercent)/100.0)
	// return converted quantity as int64, use binarySI to ensure it is displayed correct units as Mi, Gi, etc...
	return *resource.NewQuantity(int64(newBytes), resource.BinarySI)
}
