// Package gcode implements GCode file parsing and manipulation utilities.
//
// Functions:
//   - PatchGCodeTime:     Insert ;TIME:<seconds> before first G28 if not present.
//                         Parses estimated print time from OrcaSlicer/PrusaSlicer comments.
//   - ExtractLayerCount:  Extract total layer count from GCode header comments.
//                         Supports OrcaSlicer (;LAYER_COUNT:N), Bambu (; total layer number: N),
//                         and PrusaSlicer (counting ;LAYER_CHANGE markers).
//
// Python source: cli/util.py (patch_gcode_time, extract_layer_count)
package gcode
