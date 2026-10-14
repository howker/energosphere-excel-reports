package report

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
)

var twoCellAnchorRE = regexp.MustCompile(`(?s)<xdr:twoCellAnchor\b[^>]*>.*?</xdr:twoCellAnchor>`)
var schemePicRE = regexp.MustCompile(`(?s)<xdr:pic\b[^>]*>.*?</xdr:pic>`)
var schemeExtentRE = regexp.MustCompile(`<a:ext cx="[0-9]+" cy="[0-9]+"/>`)

// Размер схемы по образцу пользователя: 300 × 345 пт. Якорь одной ячейки
// исключает растяжение изображения при переносе названий и росте строк.
func fixedSchemeAnchors(data []byte) []byte {
	const width, height = 300 * 12700, 345 * 12700
	return twoCellAnchorRE.ReplaceAllFunc(data, func(anchor []byte) []byte {
		pic := schemePicRE.Find(anchor)
		if len(pic) == 0 {
			// Сдвиг всей однолинейной схемы на 6 пт освобождает место под E31.
			// Оба края сдвигаются одинаково, поэтому размер схемы не меняется.
			if bytes.Contains(anchor, []byte(`name="Группа 106"`)) {
				offsetRE := regexp.MustCompile(`<xdr:rowOff>([0-9]+)</xdr:rowOff>`)
				return offsetRE.ReplaceAllFunc(anchor, func(b []byte) []byte {
					m := offsetRE.FindSubmatch(b)
					offset, _ := strconv.Atoi(string(m[1]))
					return []byte(fmt.Sprintf(`<xdr:rowOff>%d</xdr:rowOff>`, offset+6*12700))
				})
			}
			return anchor
		}
		if !pictureRE.Match(pic) {
			return anchor
		}
		pic = schemeExtentRE.ReplaceAllLiteral(pic, []byte(fmt.Sprintf(`<a:ext cx="%d" cy="%d"/>`, width, height)))
		start := `<xdr:oneCellAnchor><xdr:from><xdr:col>17</xdr:col><xdr:colOff>942975</xdr:colOff><xdr:row>21</xdr:row><xdr:rowOff>101600</xdr:rowOff></xdr:from>`
		return []byte(start + fmt.Sprintf(`<xdr:ext cx="%d" cy="%d"/>`, width, height) + string(pic) + `<xdr:clientData/></xdr:oneCellAnchor>`)
	})
}
