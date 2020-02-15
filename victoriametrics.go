package main

func convertPromMetricToVM(buf *buffer, m *promMetric, ts int64) {
	// name + labels

	buf.writeString(`{"metric":{"__name__":"`)
	buf.write(m.name)
	buf.writeByte('"')

	for i, label := range m.labels {
		if i < len(m.labels) {
			buf.writeByte(',')
		}
		buf.writeByte('"')
		buf.write(label.key)
		buf.writeString(`":"`)
		buf.write(label.value)
		buf.writeByte('"')
	}

	buf.writeByte('}')

	// value

	buf.writeString(`,"values":[`)
	buf.write(m.value)
	buf.writeByte(']')

	// timestamp

	buf.writeString(`,"timestamps":[`)
	buf.appendInt(ts)
	buf.writeString("]}\n")
}
