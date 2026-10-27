package identity

import (
	"context"
)

// Fake answers with invented but stable data, for development and tests without
// identity-api. It is never used in production (the config requires a real key there).
// The documents "00000000" and "00000000000" are reported as not found.
type Fake struct{}

var (
	fakeFirstNames = []string{"CARLOS ALBERTO", "MARIA ELENA", "LUIS FERNANDO", "ROSA ISABEL", "JORGE LUIS", "ANA LUCIA"}
	fakePaternal   = []string{"QUISPE", "RAMOS", "TORRES", "SALAZAR", "MENDOZA", "CASTILLO"}
	fakeMaternal   = []string{"HUAMAN", "FLORES", "VEGA", "ROJAS", "PAREDES", "DIAZ"}
	fakeCompanies  = []string{"POWER FITNESS S.A.C.", "FUERZA GYM S.A.C.", "ATLAS TRAINING S.A.C.", "VITAL SPORT CENTER S.A.C."}
	fakeAddresses  = []string{"AV. LARCO 1250, MIRAFLORES", "JR. DE LA UNION 480, LIMA", "AV. ARTURO 2310, BARRANCO"}
)

func seed(documentNumber string) int {
	sum := 0
	for i, digit := range documentNumber {
		sum += int(digit-'0') * (i + 1)
	}
	return sum
}

// Person returns a stable invented person.
func (Fake) Person(_ context.Context, documentNumber string) (Person, error) {
	if documentNumber == "00000000" {
		return Person{}, ErrNotFound
	}
	n := seed(documentNumber)
	return Person{
		DocumentNumber:  documentNumber,
		FirstNames:      fakeFirstNames[n%len(fakeFirstNames)],
		PaternalSurname: fakePaternal[(n+1)%len(fakePaternal)],
		MaternalSurname: fakeMaternal[(n+2)%len(fakeMaternal)],
	}, nil
}

// Company returns a stable invented company.
func (Fake) Company(_ context.Context, documentNumber string) (Company, error) {
	if documentNumber == "00000000000" {
		return Company{}, ErrNotFound
	}
	n := seed(documentNumber)
	return Company{
		DocumentNumber: documentNumber,
		LegalName:      fakeCompanies[n%len(fakeCompanies)],
		TradeName:      "",
		Address:        fakeAddresses[(n+1)%len(fakeAddresses)],
	}, nil
}
