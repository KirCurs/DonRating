package feedback

import "testing"

type ratingTestCase struct {
	name    string
	rating  Rating
	wantErr bool
}

func TestValidateRatingErr(t *testing.T) {
	cases := []ratingTestCase{
		{
			name: "max_score",
			rating: Rating{
				RequestID: "Request-1",
				Score:     5,
			},
			wantErr: false,
		},
		{
			name: "minimum_score",
			rating: Rating{
				RequestID: "Request-1",
				Score:     1,
			},
			wantErr: false,
		},
		{
			name: "EmptyRequestID",
			rating: Rating{
				RequestID: "",
				Score:     5,
			},
			wantErr: true,
		},
		{
			name: "score_too_low",
			rating: Rating{
				RequestID: "Request-1",
				Score:     0,
			},
			wantErr: true,
		},
		{
			name: "score_too_high",
			rating: Rating{
				RequestID: "Request-1",
				Score:     6,
			},
			wantErr: true,
		},
		{
			name: "whitespace_request_id",
			rating: Rating{
				RequestID: "   ",
				Score:     5,
			}, wantErr: true,
		},
	}

	for _, tc := range cases {
		err := tc.rating.Validate()
		gotErr := err != nil

		if gotErr != tc.wantErr {
			t.Errorf("%s: ожидали ошибку=%v, получили: %v",
				tc.name, tc.wantErr, err)
		}
	}

}
