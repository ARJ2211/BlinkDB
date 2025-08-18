// 1A acceptance checks (no code yet)
//
// A) First write creates version 1
// - Set("u1", "A")
// - Get("u1") => value="A", version=1
// - createdAt == updatedAt (roughly equal, non-zero)
//
// B) Second write bumps version and updatedAt
// - Set("u1", "B")
// - Get("u1") => value="B", version=2
// - createdAt unchanged; updatedAt > previous updatedAt
//
// C) Get missing key
// - Get("missing") => found=false
//
// D) Delete existing key
// - Delete("u1") => true
// - Get("u1") => found=false
//
// E) Delete missing key is idempotent
// - Delete("u1") => false
//
// F) Size reflects live keys
// - After inserting k1,k2 and deleting k1 => Size()==1
//
// G) (Optional) Keys has exactly current keys; order not asserted
// - Insert k1,k2; Keys() contains both; no duplicates

package store
