package flow

func paginate(pageNum int, pageSize int, sliceLength int) (start, end int) {
	start = (pageNum - 1) * pageSize
	if start > sliceLength {
		start = sliceLength
	}
	end = start + pageSize
	if end > sliceLength {
		end = sliceLength
	}
	return
}

type Base struct {
	Total int `json:"total"`
	Pages int `json:"pages"`
	Page  int `json:"page"`
	Size  int `json:"size"`
}
