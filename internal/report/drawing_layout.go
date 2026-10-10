package report

import (
	"fmt"
	"regexp"
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
		if len(pic) == 0 || !pictureRE.Match(pic) {
			return anchor
		}
		pic = schemeExtentRE.ReplaceAllLiteral(pic, []byte(fmt.Sprintf(`<a:ext cx="%d" cy="%d"/>`, width, height)))
		start := `<xdr:oneCellAnchor><xdr:from><xdr:col>17</xdr:col><xdr:colOff>368300</xdr:colOff><xdr:row>21</xdr:row><xdr:rowOff>101600</xdr:rowOff></xdr:from>`
		return []byte(start + fmt.Sprintf(`<xdr:ext cx="%d" cy="%d"/>`, width, height) + string(pic) + `<xdr:clientData/></xdr:oneCellAnchor>`)
	})
}
