package model

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Cauldron is the model of a cauldron: a floor raised a quarter of a block and four thin walls.
type Cauldron struct{}

// BBox ...
func (Cauldron) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	bbox := []cube.BBox{full.ExtendTowards(cube.FaceUp, -0.75)}
	for _, f := range cube.HorizontalFaces() {
		bbox = append(bbox, full.ExtendTowards(f, -0.875))
	}
	return bbox
}

// FaceSolid only returns true for the top face of the cauldron, its rim.
func (Cauldron) FaceSolid(_ cube.Pos, face cube.Face, _ world.BlockSource) bool {
	return face == cube.FaceUp
}
