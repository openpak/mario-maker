package smm1

// groundTable maps the eight-neighbour occupancy of a ground tile to the
// "extended data" value the game stores in the tile (its auto-tile index).
//
// The index bits are, from the most significant bit: north, south, east, west,
// north-east, north-west, south-east, south-west (1 = another ground tile, a
// start/goal piece or the world floor is there). Each entry is the most common
// value observed for that neighbourhood in real courses (the MIT-licensed test
// courses of Tarnadas/smmdb-lib, six courses, about 7,000 ground tiles). The game
// draws several visual variants for some neighbourhoods; the entry is the plain
// one. Neighbourhoods that never occur in the samples take the value of the
// nearest observed neighbourhood. Only the data values are used, no code.
var groundTable = [256]uint8{
	24, 24, 24, 30, 24, 24, 24, 24, 24, 24, 24, 24, 24, 24, 24, 24,
	27, 27, 27, 66, 27, 27, 27, 27, 27, 27, 27, 27, 27, 27, 27, 27,
	25, 25, 25, 64, 25, 25, 25, 25, 25, 25, 25, 25, 25, 25, 25, 25,
	26, 26, 26, 26, 26, 26, 7, 26, 26, 26, 7, 26, 26, 26, 26, 26,
	28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28,
	33, 60, 33, 60, 33, 60, 33, 60, 33, 60, 33, 60, 33, 60, 60, 60,
	34, 34, 58, 58, 34, 34, 58, 58, 34, 34, 58, 58, 34, 34, 58, 58,
	36, 45, 44, 59, 36, 59, 44, 59, 36, 45, 44, 59, 44, 45, 44, 59,
	30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30,
	31, 31, 31, 31, 66, 66, 66, 66, 31, 31, 31, 66, 66, 66, 66, 66,
	32, 32, 32, 32, 32, 32, 32, 64, 64, 64, 64, 64, 64, 64, 64, 64,
	35, 35, 35, 35, 47, 47, 47, 47, 46, 46, 46, 46, 65, 65, 65, 65,
	29, 29, 29, 29, 29, 29, 29, 29, 29, 29, 29, 29, 29, 29, 29, 29,
	37, 41, 37, 41, 43, 3, 43, 63, 37, 41, 37, 41, 43, 3, 43, 3,
	38, 38, 40, 40, 38, 38, 40, 40, 42, 61, 2, 61, 42, 42, 2, 2,
	39, 52, 52, 52, 51, 55, 56, 68, 50, 57, 54, 67, 53, 70, 69, 62,
}
