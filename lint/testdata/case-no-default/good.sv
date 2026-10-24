module top (input [1:0] s, input [3:0] a, b, output logic [3:0] y);
  always_comb begin
    case (s)
      0: y = a;
      default: y = b;
    endcase
    unique case (s)
      0: y = a;
      1: y = b;
      2, 3: y = 0;
    endcase
    priority casez (s)
      2'b1?: y = a;
      2'b0?: y = b;
    endcase
  end
endmodule
