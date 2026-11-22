package proto

// H5GuildView returns the rules and quotes needed by the H5 guild screens.
// The original guild commands still own all mutations.
const H5GuildView uint16 = 13550

func registerH5GuildSchema(s *Schema) {
	s.Send[H5GuildView] = []Field{{Name: "view", Type: "uint8"}, {Name: "career", Type: "uint8"}}
	s.Recv[H5GuildView] = []Field{{Name: "code", Type: "uint8"}, {Name: "msg", Type: "string"}, {Name: "data", Type: "string"}}
}
