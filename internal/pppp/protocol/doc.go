// Package protocol defines the PPPP (peer-to-peer) packet types and
// message parsing for LAN communication with AnkerMake printers.
//
// PPPP is an asymmetric UDP-based protocol with 8 logical channels,
// supporting DRW (data) packets with ACK-based reliability, Xzyh frames
// for command/video data, and Aabb frames for file transfers with CRC.
//
// Key types: Message, PktDrw, PktDrwAck, PktClose, PktLanSearch,
// Xzyh, Aabb, FileTransfer, Duid, Host, CyclicU16.
//
// Python sources: libflagship/pppp.py, libflagship/cyclic.py
package protocol
